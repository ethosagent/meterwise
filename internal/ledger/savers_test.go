package ledger_test

import (
	"context"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// runSavers drives one call through Before and After with the given usage and returns its record and the hook status.
func runSavers(t *testing.T, c *tollgate.Call, mod func(*tollgate.Metric)) (contract.CallRecord, map[string]any) {
	t.Helper()
	w := &fakeWriter{}
	h := newHook(t, w, nil)
	if _, err := h.Before(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	m := metricFor(c)
	if mod != nil {
		mod(&m)
	}
	h.After(context.Background(), c, m)
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := w.snapshot()
	if len(got) != 1 {
		t.Fatalf("records = %d", len(got))
	}
	return got[0], h.Status()
}

func usage(in, out int64) func(*tollgate.Metric) {
	return func(m *tollgate.Metric) { m.Usage = tollgate.Usage{InputTokens: in, OutputTokens: out} }
}

func trimMeta(action contract.TrimAction, before, after int, stages ...contract.TrimStageResult) contract.TrimResult {
	return contract.TrimResult{Mode: contract.TrimModeLossless, Action: action, BodyBefore: before, BodyAfter: after, Stages: stages}
}

func stage(s contract.TrimStage, before, after int, reverted bool) contract.TrimStageResult {
	return contract.TrimStageResult{Stage: s, Tool: "bash", Regions: 1, BytesBefore: before, BytesAfter: after, Reverted: reverted}
}

func TestTrimIsRecordedPerTheTokenAndConfidenceRules(t *testing.T) {
	tests := []struct {
		name       string
		wire       tollgate.WireFormat
		provider   string
		model      string
		action     contract.TrimAction
		usage      int64
		before     int
		after      int
		wantTokens int64
		wantUSD    contract.MicroUSD
		wantConf   contract.Confidence
	}{
		// 1000 bytes saved of 3000 sent, 3000 input tokens: 1000 tokens at 2.5 USD/M = 2500 micro-USD
		{"openai acted with reported input is certain", tollgate.WireOpenAIChat, "openai", "gpt-4o", contract.TrimActed, 3000, 4000, 3000, 1000, 2500, contract.ConfidenceCertain},
		{"anthropic acted is likely", tollgate.WireAnthropicMessages, "anthropic", "claude-sonnet-4-5", contract.TrimActed, 3000, 4000, 3000, 1000, 3000, contract.ConfidenceLikely},
		// observe: sent is the untouched body (4000): ceil(1000*3000/4000) = 750
		{"observe is a would-save, likely", tollgate.WireOpenAIChat, "openai", "gpt-4o", contract.TrimObserve, 3000, 4000, 3000, 750, 1875, contract.ConfidenceLikely},
		{"no reported input falls back to a quarter and is likely", tollgate.WireOpenAIChat, "openai", "gpt-4o", contract.TrimActed, 0, 4000, 3600, 100, 250, contract.ConfidenceLikely},
		{"an unpriced model keeps the tokens and a zero USD", tollgate.WireOpenAIChat, "openai", "no-such-model", contract.TrimActed, 3000, 4000, 3000, 1000, 0, contract.ConfidenceCertain},
		{"a body that grew saves nothing", tollgate.WireOpenAIChat, "openai", "gpt-4o", contract.TrimActed, 3000, 3000, 3100, 0, 0, contract.ConfidenceCertain},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := directCall("c1", "hi", nil, 1)
			c.Provider.Wire = tc.wire
			c.Meta[contract.MetaTrim] = trimMeta(tc.action, tc.before, tc.after)
			rec, st := runSavers(t, c, func(m *tollgate.Metric) {
				m.Provider, m.Model = tc.provider, tc.model
				m.Usage = tollgate.Usage{InputTokens: tc.usage, OutputTokens: 5}
			})
			if rec.TrimAction == nil || *rec.TrimAction != tc.action || rec.TrimConfidence == nil || *rec.TrimConfidence != tc.wantConf {
				t.Fatalf("action/confidence = %v/%v, want %s/%s", rec.TrimAction, rec.TrimConfidence, tc.action, tc.wantConf)
			}
			if rec.TrimTokens != tc.wantTokens || rec.SavedUSDTrim != tc.wantUSD {
				t.Fatalf("tokens/usd = %d/%d, want %d/%d", rec.TrimTokens, rec.SavedUSDTrim, tc.wantTokens, tc.wantUSD)
			}
			if tc.model == "no-such-model" && st["trim_unpriced"] != int64(1) {
				t.Errorf("trim_unpriced = %v", st["trim_unpriced"])
			}
		})
	}
}

func TestTrimEventsAreOnePerNonRevertedStageAndSumToTheCallsTokens(t *testing.T) {
	c := directCall("c1", "hi", nil, 1)
	c.Meta[contract.MetaTrim] = trimMeta(contract.TrimActed, 4000, 3000,
		stage(contract.StageRepeatCollapse, 600, 200, false), // saved 400
		stage(contract.StageLogWindow, 500, 400, true),       // reverted: no row
		stage(contract.StageDupLines, 900, 300, false),       // saved 600
		stage(contract.StageDupLines, 100, 50, false),        // a repeated stage name: the first wins
		stage("bogus", 100, 50, false),                       // not a stage: dropped
	)
	rec, _ := runSavers(t, c, usage(3000, 5))
	if len(rec.TrimEvents) != 2 {
		t.Fatalf("events = %+v", rec.TrimEvents)
	}
	var sum int64
	for _, e := range rec.TrimEvents {
		sum += e.EstTokens
		if e.Reverted || e.Action != contract.TrimActed || e.CallID != "c1" || e.WorkspaceID != wsID || e.Tool != "bash" {
			t.Errorf("event = %+v", e)
		}
	}
	if sum != rec.TrimTokens || rec.TrimTokens != 1000 {
		t.Fatalf("stage tokens sum %d, call tokens %d, want 1000", sum, rec.TrimTokens)
	}
	if rec.TrimEvents[0].EstTokens != 400 || rec.TrimEvents[1].EstTokens != 600 {
		t.Fatalf("pro rata by bytes saved: %+v", rec.TrimEvents)
	}
}

func TestNothingIsRecordedForACallThatNeverReachedTheUpstream(t *testing.T) {
	meta := func(c *tollgate.Call) {
		c.Meta[contract.MetaTrim] = trimMeta(contract.TrimActed, 4000, 3000, stage(contract.StageDupLines, 900, 300, false))
		c.Meta[contract.MetaRouter] = contract.RouteDecision{Action: contract.RouteRouted, From: "gpt-4o", To: "gpt-4o-mini"}
		c.Meta[contract.MetaMemory] = contract.MemoryHint{Injected: true}
	}
	zero := directCall("c1", "hi", nil, 1)
	meta(zero)
	rec, _ := runSavers(t, zero, usage(0, 0))
	if rec.TrimAction != nil || rec.RouterAction != nil || rec.HintInjected || len(rec.TrimEvents) != 0 || rec.SavedUSDTrim != 0 || rec.SavedUSDRouter != 0 {
		t.Errorf("zero usage recorded savers: %+v", rec)
	}
	blocked, cv := verdictCall("c2", mv(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeBlock))
	_ = cv
	meta(blocked)
	rec, _ = runSavers(t, blocked, usage(0, 0))
	if !rec.Blocked || rec.TrimAction != nil || rec.RouterAction != nil {
		t.Errorf("a blocked call recorded savers: %+v", rec)
	}
}

func TestRouterSavingsAreThePriceDifferenceAtTheCallsUsage(t *testing.T) {
	routed := func(from, to string) contract.RouteDecision {
		return contract.RouteDecision{Mode: contract.RouterModeConservative, Action: contract.RouteRouted, From: from, To: to, ScoreBp: 1200}
	}
	run := func(model string, d contract.RouteDecision) (contract.CallRecord, map[string]any) {
		c := directCall("c1", "hi", nil, 1)
		c.Model = model
		c.Meta[contract.MetaRouter] = d
		return runSavers(t, c, func(m *tollgate.Metric) {
			m.Model = model
			m.Usage = tollgate.Usage{InputTokens: 1_000_000, OutputTokens: 100_000}
		})
	}
	// routed: the call is priced at the target (the real bill); saved = Cost(from) - Cost(to)
	// gpt-4o 1M in + 0.1M out = 2.5 + 1.0 = 3.5 USD; gpt-4o-mini = 0.15 + 0.06 = 0.21 USD; saved 3.29 USD
	rec, _ := run("gpt-4o-mini", routed("gpt-4o", "gpt-4o-mini"))
	if rec.RouterAction == nil || *rec.RouterAction != contract.RouteRouted || rec.RoutedFrom != "gpt-4o" || rec.RoutedTo != "gpt-4o-mini" ||
		rec.RouterScoreBp == nil || *rec.RouterScoreBp != 1200 || rec.SavedUSDRouter != 3_290_000 {
		t.Fatalf("routed = %+v saved %d", rec, rec.SavedUSDRouter)
	}
	// observe: the call ran on its own model; would-save = Cost(model) - Cost(to), recorded with its action
	rec, _ = run("gpt-4o", contract.RouteDecision{Action: contract.RouteObserve, From: "gpt-4o", To: "gpt-4o-mini", ScoreBp: 99999})
	if rec.RouterAction == nil || *rec.RouterAction != contract.RouteObserve || rec.SavedUSDRouter != 3_290_000 || *rec.RouterScoreBp != 10000 {
		t.Fatalf("observe = %+v saved %d score %d", rec.RouterAction, rec.SavedUSDRouter, *rec.RouterScoreBp)
	}
	// held changes nothing; an unknown action is dropped and counted
	rec, st := run("gpt-4o", contract.RouteDecision{Action: contract.RouteHeld, Reason: contract.HeldCapability})
	if rec.RouterAction != nil || rec.SavedUSDRouter != 0 || st["invalid_saver_meta"] != int64(0) {
		t.Fatalf("held = %+v %v", rec, st)
	}
	rec, st = run("gpt-4o", contract.RouteDecision{Action: "weird", To: "gpt-4o-mini"})
	if rec.RouterAction != nil || st["invalid_saver_meta"] != int64(1) {
		t.Fatalf("invalid = %+v %v", rec.RouterAction, st)
	}
	// both models must price: otherwise 0 and counted
	rec, st = run("brand-new", routed("gpt-4o", "brand-new"))
	if rec.RouterAction == nil || rec.SavedUSDRouter != 0 || st["router_unpriced"] != int64(1) {
		t.Fatalf("unpriced = %+v saved %d %v", rec.RouterAction, rec.SavedUSDRouter, st)
	}
}

func TestInjectedHintIsRecordedAndAnInvalidTrimIsCounted(t *testing.T) {
	c := directCall("c1", "hi", nil, 1)
	c.Meta[contract.MetaMemory] = &contract.MemoryHint{Mode: contract.MemoryModeHint, Injected: true}
	c.Meta[contract.MetaTrim] = &contract.TrimResult{Action: "weird"}
	rec, st := runSavers(t, c, nil)
	if !rec.HintInjected || rec.TrimAction != nil || st["invalid_saver_meta"] != int64(1) {
		t.Fatalf("hint %v trim %v status %v", rec.HintInjected, rec.TrimAction, st["invalid_saver_meta"])
	}
	c = directCall("c2", "hi", nil, 1)
	c.Meta[contract.MetaMemory] = contract.MemoryHint{Mode: contract.MemoryModeObserve, Injected: false}
	if rec, _ = runSavers(t, c, nil); rec.HintInjected {
		t.Fatal("an observed hint was not injected")
	}
}

func TestWrongTypedSaverMetaIsCounted(t *testing.T) {
	c := directCall("w1", "hi", nil, 1)
	c.Meta[contract.MetaTrim] = "not a TrimResult"
	c.Meta[contract.MetaRouter] = 42
	c.Meta[contract.MetaMemory] = struct{}{}
	rec, st := runSavers(t, c, nil)
	if rec.TrimAction != nil || rec.RouterAction != nil || rec.HintInjected || st["invalid_saver_meta"] != int64(3) {
		t.Fatalf("rec %+v status %v", rec, st["invalid_saver_meta"])
	}
}
