package ledger_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
)

// verdictCall is a call whose history stage put a verdict collector in Meta and whose monitors wrote verdicts.
func verdictCall(id string, vs ...contract.MonitorVerdict) (*tollgate.Call, *contract.CallVerdicts) {
	c := directCall(id, "hi", nil, 1)
	cv := &contract.CallVerdicts{}
	for _, v := range vs {
		cv.Add(v)
	}
	c.Meta[contract.MetaMonitors] = cv
	return c, cv
}

func mv(m contract.MonitorName, mode, action contract.MonitorMode) contract.MonitorVerdict {
	return contract.MonitorVerdict{Monitor: m, Mode: mode, Action: action, Evidence: contract.MonitorEvidence{Tool: "read", ArgsHash: "ab12", Count: 3, Threshold: 2}}
}

// estimate is a fixed estimator: block -> 1M input tokens certain, note -> 100k likely, observe -> 50k likely.
func estimate(v contract.MonitorVerdict) (contract.Usage, contract.Confidence) {
	switch v.Action {
	case contract.ModeBlock:
		return contract.Usage{Input: 1_000_000}, contract.ConfidenceCertain
	case contract.ModeNote:
		return contract.Usage{Input: 100_000}, contract.ConfidenceLikely
	}
	return contract.Usage{Input: 50_000}, contract.ConfidenceLikely
}

// run drives Before/After by hand: usage false gives the Metric zero usage (a short-circuited block).
func run(t *testing.T, mod func(*ledger.Options), c *tollgate.Call, usage bool) (contract.CallRecord, map[string]any) {
	t.Helper()
	w := &fakeWriter{}
	h := newHook(t, w, mod)
	if _, err := h.Before(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	m := metricFor(c)
	if !usage {
		m.Usage = tollgate.Usage{}
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

func TestVerdictsArePricedPerRulingR6(t *testing.T) {
	c, cv := verdictCall("c1",
		mv(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeBlock),
		mv(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeNote),
		mv(contract.MonitorNoProgress, contract.ModeObserve, contract.ModeObserve),
	)
	cv.AddNotes(1)
	rec, st := run(t, func(o *ledger.Options) { o.Estimator = estimate }, c, false)
	if !rec.Blocked || rec.NotesInjected != 1 || len(rec.Verdicts) != 3 {
		t.Fatalf("record = blocked %v notes %d verdicts %d", rec.Blocked, rec.NotesInjected, len(rec.Verdicts))
	}
	want := []struct {
		tokens int64
		usd    contract.MicroUSD
		conf   contract.Confidence
		saved  bool
	}{
		{1_000_000, 2_500_000, contract.ConfidenceCertain, true}, // 1M gpt-4o input tokens at $2.50/M
		{100_000, 250_000, contract.ConfidenceLikely, true},
		{50_000, 125_000, contract.ConfidenceLikely, false}, // observe: would-save, in neither number
	}
	for i, w := range want {
		v := rec.Verdicts[i]
		if v.EstTokens != w.tokens || v.EstUSD != w.usd || v.Confidence != w.conf || v.CountsAsSaved() != w.saved {
			t.Errorf("verdict %d = %+v, want %+v", i, v, w)
		}
	}
	if st["blocks_downgraded"] != int64(0) || st["invalid_verdicts"] != int64(0) {
		t.Errorf("status = %v", st)
	}
}

func TestDefaultEstimatorFindsNothing(t *testing.T) {
	c, _ := verdictCall("c1", mv(contract.MonitorStepBudget, contract.ModeNote, contract.ModeNote))
	rec, _ := run(t, nil, c, true)
	v := rec.Verdicts[0]
	if v.EstTokens != 0 || v.EstUSD != 0 || v.Confidence != contract.ConfidenceNone {
		t.Fatalf("verdict = %+v", v)
	}
}

// A block that lost the race (the call already has usage: it reached the upstream) is observe, counted, and never Blocked.
func TestLateBlockIsDowngradedToObserve(t *testing.T) {
	c, _ := verdictCall("c1", mv(contract.MonitorDuplicateToolCall, contract.ModeBlock, contract.ModeBlock))
	rec, st := run(t, func(o *ledger.Options) { o.Estimator = estimate }, c, true)
	v := rec.Verdicts[0]
	if rec.Blocked || v.Action != contract.ModeObserve || v.Mode != contract.ModeBlock || v.CountsAsSaved() {
		t.Fatalf("record blocked=%v verdict=%+v", rec.Blocked, v)
	}
	if st["blocks_downgraded"] != int64(1) {
		t.Errorf("status = %v", st)
	}
}

func TestVerdictsAreSealedAndInvalidOnesDropped(t *testing.T) {
	c, cv := verdictCall("c1",
		mv("made_up", contract.ModeNote, contract.ModeNote),
		mv(contract.MonitorRepeatedRead, contract.ModeNote, "weird"),
		mv(contract.MonitorNoProgress, contract.ModeNote, contract.ModeNote),
	)
	rec, st := run(t, nil, c, true)
	if len(rec.Verdicts) != 1 || rec.Verdicts[0].Monitor != contract.MonitorNoProgress || st["invalid_verdicts"] != int64(2) {
		t.Fatalf("verdicts = %+v status %v", rec.Verdicts, st)
	}
	if cv.Add(mv(contract.MonitorSecondGuessing, contract.ModeNote, contract.ModeNote)) {
		t.Error("a verdict was accepted after the ledger sealed the call")
	}
}

func TestNoVerdictPointerMeansNoVerdicts(t *testing.T) {
	rec, _ := run(t, nil, directCall("c1", "hi", nil, 1), true)
	if len(rec.Verdicts) != 0 || rec.Blocked || rec.NotesInjected != 0 {
		t.Fatalf("record = %+v", rec)
	}
}

func TestEvidenceIsCleanedAndPathNeedsCapture(t *testing.T) {
	evidence := func() contract.MonitorVerdict {
		v := mv(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeNote)
		v.Evidence = contract.MonitorEvidence{Tool: "re\x00ad" + strings.Repeat("x", 300), Path: "/src/main.go", PathHash: "ph", Phrases: []string{"wait\x00 no"}}
		return v
	}
	off := false
	tests := []struct {
		name     string
		policy   *tollgate.Policy
		wantPath string
	}{
		{"privacy on drops the path", nil, ""},
		{"capture without global opt-out drops it", &tollgate.Policy{Capture: true}, ""},
		{"capture allowed keeps it", &tollgate.Policy{Capture: true, PrivacyMode: &off}, "/src/main.go"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := verdictCall("c1", evidence())
			c.Tenant.Policy = tc.policy
			rec, _ := run(t, func(o *ledger.Options) {
				if tc.wantPath != "" {
					o.PrivacyMode = &off
				}
			}, c, true)
			e := rec.Verdicts[0].Evidence
			if e.Path != tc.wantPath || e.PathHash != "ph" {
				t.Errorf("path = %q pathHash = %q", e.Path, e.PathHash)
			}
			if strings.ContainsRune(e.Tool, 0) || len(e.Tool) > 128 || strings.ContainsRune(e.Phrases[0], 0) {
				t.Errorf("evidence not cleaned: %+v", e)
			}
		})
	}
}

func TestUnpricedModelKeepsTokensWithZeroUSD(t *testing.T) {
	c, _ := verdictCall("c1", mv(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeNote))
	c.Model = "no-such-model"
	rec, _ := run(t, func(o *ledger.Options) { o.Estimator = estimate }, c, true)
	if v := rec.Verdicts[0]; v.EstTokens != 100_000 || v.EstUSD != 0 {
		t.Fatalf("verdict = %+v", v)
	}
}

// verdictHook stands in for history + monitors: it puts the collector in Meta (Before, adopted, as history does)
// and adds the verdict its table names for the call's 1-based index in its session.
type verdictHook struct {
	tollgate.NoopHook
	byIndex map[int][]contract.MonitorVerdict
}

func (verdictHook) Name() string { return "test-monitors" }

func (v verdictHook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	cv := &contract.CallVerdicts{}
	for _, mv := range v.byIndex[c.Session.Calls] {
		cv.Add(mv)
	}
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	c.Meta[contract.MetaMonitors] = cv
	return nil, nil
}

// byMonitor prices by monitor so the sums are easy to reconcile: duplicate_tool_call is likely 100k input tokens,
// repeated_read is certain 200k, anything else finds nothing.
func byMonitor(v contract.MonitorVerdict) (contract.Usage, contract.Confidence) {
	switch v.Monitor {
	case contract.MonitorDuplicateToolCall:
		return contract.Usage{Input: 100_000}, contract.ConfidenceLikely
	case contract.MonitorRepeatedRead:
		return contract.Usage{Input: 200_000}, contract.ConfidenceCertain
	}
	return contract.Usage{}, contract.ConfidenceNone
}

// Real Postgres, real tollgate: verdicts become monitor_events rows and the session sums reconcile with them
// per R6 (certain acted -> saved, likely acted -> likely, observe in neither); a replayed call writes nothing twice.
func TestRealPostgresVerdictsReconcileWithSessionSums(t *testing.T) {
	s, ws := pgStore(t)
	h := newHook(t, s, func(o *ledger.Options) { o.Estimator = byMonitor })
	vh := verdictHook{byIndex: map[int][]contract.MonitorVerdict{
		1: {mv(contract.MonitorDuplicateToolCall, contract.ModeNote, contract.ModeNote)},
		2: {mv(contract.MonitorDuplicateToolCall, contract.ModeObserve, contract.ModeObserve), mv(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeNote)},
		3: {mv(contract.MonitorRepeatedRead, contract.ModeObserve, contract.ModeObserve), mv(contract.MonitorStepBudget, contract.ModeNote, contract.ModeNote)},
	}}
	fx := []string{"openai/chat-stream-text", "openai/chat-stream-text", "openai/chat-stream-text"}
	testkit.RunWith(t, testkit.Config{Plugins: []any{tenantAuth{ws: ws}, vh, h}, Fixtures: fx})
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	events := scalar[int](t, s, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1`, ws)
	if events != 5 {
		t.Fatalf("events = %d, want 5", events)
	}
	// the fixture's model is gpt-4.1-mini ($0.40/M input): duplicate note is likely 100k = 0.04, repeated_read note is
	// certain 200k = 0.08; the observe rows are would-save
	saved := scalar[string](t, s, `SELECT COALESCE(SUM(est_saved_usd), 0)::text FROM monitor_events WHERE workspace_id = $1 AND action <> 'observe' AND confidence = 'certain'`, ws)
	likely := scalar[string](t, s, `SELECT COALESCE(SUM(est_saved_usd), 0)::text FROM monitor_events WHERE workspace_id = $1 AND action <> 'observe' AND confidence = 'likely'`, ws)
	if saved != "0.080000" || likely != "0.040000" {
		t.Fatalf("event sums saved=%s likely=%s", saved, likely)
	}
	sess := scalar[string](t, s, `SELECT saved_usd::text || '/' || likely_saved_usd::text FROM sessions WHERE workspace_id = $1`, ws)
	if sess != "0.080000/0.040000" {
		t.Fatalf("session sums = %s, want 0.080000/0.040000: observe must be in neither", sess)
	}
	if n := scalar[int](t, s, `SELECT count(*) FROM calls WHERE workspace_id = $1 AND notes_injected = 0 AND blocked`, ws); n != 0 {
		t.Fatalf("%d calls blocked without a block verdict", n)
	}
}

// A call recorded twice (a retry of the same tollgate call id) stores its events once and moves no sum.
func TestRealPostgresReplayedCallWritesNoSecondEvent(t *testing.T) {
	s, ws := pgStore(t)
	h := newHook(t, s, func(o *ledger.Options) { o.Estimator = byMonitor })
	for range 2 {
		c, _ := verdictCall("same-call", mv(contract.MonitorRepeatedRead, contract.ModeNote, contract.ModeNote))
		c.Tenant.ID = ws
		complete(t, h, c)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int](t, s, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1`, ws); n != 1 {
		t.Fatalf("events = %d, want 1", n)
	}
	if got := scalar[string](t, s, `SELECT saved_usd::text FROM sessions WHERE workspace_id = $1`, ws); got != "0.500000" {
		t.Fatalf("saved = %s, want 0.500000 once", got)
	}
}
