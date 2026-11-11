package trim_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/trim"
	"github.com/ethosagent/meterwise/internal/wire"
)

// samePrefix fails unless next starts with everything prev sent up to the end of its messages.
func samePrefix(t *testing.T, label string, prev, next []byte) {
	t.Helper()
	end := len(prev) - 2 // the closing "]}" of the messages array and the body
	if bytes.HasPrefix(next, prev[:end]) {
		return
	}
	i := 0
	for i < end && i < len(next) && prev[i] == next[i] {
		i++
	}
	t.Errorf("%s: the sent bytes diverge at %d\n prev ...%s\n next ...%s", label, i, clip(prev, i), clip(next, i))
}

func clip(b []byte, at int) string {
	lo, hi := max(0, at-40), min(len(b), at+60)
	return string(b[lo:hi])
}

func TestHookOnlyActsOnTheTwoWires(t *testing.T) {
	h := trim.New(calm())
	c := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless).send(agentSteps()[:1])
	if _, ok := result(c); !ok {
		t.Fatal("openai.chat is trimmed")
	}
	// openai.responses is DEFERRED (R12): tollgate v0.1.0 has no constant for it, so it is named by its wire string.
	for _, w := range []tollgate.WireFormat{tollgate.WirePassthrough, tollgate.WireFormat("openai.responses")} {
		s := newSession(t, h, w, contract.TrimModeLossless)
		c := s.send(agentSteps()[:1])
		if _, ok := result(c); ok || !bytes.Equal(c.Body, requestBody(t, w, agentSteps()[:1], -1, false)) {
			t.Errorf("%s was touched", w)
		}
	}
	s := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeOff)
	if c := s.send(agentSteps()); !bytes.Equal(c.Body, requestBody(t, s.w, agentSteps(), -1, false)) || len(c.Meta) != 0 {
		t.Error("off must leave the call alone")
	}
	s = newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
	s.id = ""
	if _, ok := result(s.send(agentSteps())); ok {
		t.Error("a call with no session is left alone")
	}
}

// T4: observe changes no byte and records what it would have saved.
func TestObserveLeavesBodyByteIdenticalAndReportsWouldSave(t *testing.T) {
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			h := trim.New(calm())
			s := newSession(t, h, w, contract.TrimModeObserve)
			orig := requestBody(t, w, agentSteps(), -1, false)
			c := s.send(agentSteps())
			if !bytes.Equal(c.Body, orig) {
				t.Fatal("observe changed the body")
			}
			r, ok := result(c)
			if !ok || r.Action != contract.TrimObserve || r.Mode != contract.TrimModeObserve || r.BodyBefore != len(orig) || r.BodyAfter >= r.BodyBefore {
				t.Fatalf("result = %+v", r)
			}
			// What lossless sends for the same request is as long as observe says it would have been.
			l := newSession(t, trim.New(calm()), w, contract.TrimModeLossless).send(agentSteps())
			if r.BodyAfter != len(l.Body) {
				t.Errorf("observe would-be size %d, lossless sent %d", r.BodyAfter, len(l.Body))
			}
			if st := h.Status(); st["observed"] != int64(1) || st["acted"] != int64(0) {
				t.Errorf("status = %v", st)
			}
			// the same call again reports the same would-save, from the memo
			if r2, _ := result(s.send(agentSteps())); r2.BodyAfter != r.BodyAfter {
				t.Errorf("second observe: %d vs %d", r2.BodyAfter, r.BodyAfter)
			}
		})
	}
}

func TestLosslessRewritesOnlyToolResults(t *testing.T) {
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			s := newSession(t, trim.New(calm()), w, contract.TrimModeLossless)
			s.cache = 1
			orig := requestBody(t, w, agentSteps(), 1, false)
			c := s.send(agentSteps())
			r, ok := result(c)
			if !ok || r.Action != contract.TrimActed || r.BodyBefore != len(orig) || r.BodyAfter != len(c.Body) || r.BodyAfter >= r.BodyBefore/2 {
				t.Fatalf("result = %+v (body %d -> %d)", r, len(orig), len(c.Body))
			}
			// T2: the system prompt, the tool definitions and every byte outside a tool result are untouched.
			regs, err := wire.ToolResults(w, orig)
			if err != nil {
				t.Fatal(err)
			}
			var outside bytes.Buffer
			at := 0
			for _, g := range regs {
				outside.Write(orig[at:g.Start])
				at = g.End
			}
			outside.Write(orig[at:])
			newRegs, err := wire.ToolResults(w, c.Body)
			if err != nil || len(newRegs) != len(regs) {
				t.Fatalf("regions after = %d, before = %d (%v)", len(newRegs), len(regs), err)
			}
			var outside2 bytes.Buffer
			at = 0
			for _, g := range newRegs {
				outside2.Write(c.Body[at:g.Start])
				at = g.End
			}
			outside2.Write(c.Body[at:])
			if !bytes.Equal(outside.Bytes(), outside2.Bytes()) {
				t.Error("a byte outside the tool results changed")
			}
			if !strings.Contains(string(c.Body), `"description":"Run a command"`) {
				t.Error("tool definition lost")
			}
			stages := map[contract.TrimStage]contract.TrimStageResult{}
			for _, sr := range r.Stages {
				stages[sr.Stage] = sr
			}
			for _, want := range []contract.TrimStage{contract.StageRepeatCollapse, contract.StageLogWindow, contract.StageDupLines, contract.StageJSONTable} {
				if sr, ok := stages[want]; !ok || sr.Reverted || sr.Regions == 0 || sr.BytesAfter >= sr.BytesBefore {
					t.Errorf("stage %s = %+v", want, sr)
				}
			}
			if stages[contract.StageLogWindow].Tool != "bash" {
				t.Errorf("tool = %q", stages[contract.StageLogWindow].Tool)
			}
		})
	}
}

// T3 (R6, A5): in a 6-call session the bytes sent for the shared messages never change from one call to the next.
func TestSessionPrefixIsByteIdenticalAcrossCalls(t *testing.T) {
	for _, w := range wires {
		for _, cache := range []int{-1, 1, 3} {
			t.Run(string(w), func(t *testing.T) {
				s := newSession(t, trim.New(calm()), w, contract.TrimModeLossless)
				s.cache = cache
				steps := agentSteps()
				var prev []byte
				for n := 1; n <= len(steps); n++ {
					c := s.send(steps[:n])
					if prev != nil {
						samePrefix(t, "call "+itoa(n), prev, c.Body)
					}
					if r, ok := result(c); !ok || r.BodyAfter >= r.BodyBefore {
						t.Errorf("call %d trimmed nothing: %+v", n, r)
					}
					prev = c.Body
				}
			})
		}
	}
}

func itoa(n int) string { return string(rune('0' + n)) }

// A5: the decision for a region does not depend on where the cache breakpoint sits, so moving it as the conversation
// grows never flips a region.
func TestDecisionIgnoresTheCacheBreakpoint(t *testing.T) {
	steps := agentSteps()
	var want []byte
	for _, cache := range []int{-1, 0, 2, 5} {
		s := newSession(t, trim.New(calm()), tollgate.WireAnthropicMessages, contract.TrimModeLossless)
		s.cache = cache
		regs, _ := wire.ToolResults(tollgate.WireAnthropicMessages, s.send(steps).Body)
		var texts bytes.Buffer
		for _, g := range regs {
			texts.WriteString(g.Text + "\x00")
		}
		if want == nil {
			want = texts.Bytes()
		} else if !bytes.Equal(want, texts.Bytes()) {
			t.Errorf("breakpoint at %d changed what is sent for the results", cache)
		}
	}
}

// A5: a stage enabled in the middle of a session, or observe turning into lossless, never rewrites what an earlier
// call already sent; the new setting applies to regions first seen after it.
func TestEnablingAStageMidSessionKeepsThePrefix(t *testing.T) {
	steps := agentSteps()
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			h := trim.New(calm())
			s := newSession(t, h, w, contract.TrimModeLossless)
			s.cfg.Stages[contract.StageJSONTable] = false
			s.cfg.Stages[contract.StageLogWindow] = false
			var prev []byte
			for n := 1; n <= len(steps); n++ {
				if n == 3 {
					s.cfg.Stages[contract.StageLogWindow] = true
				}
				if n == 5 {
					s.cfg.Stages[contract.StageJSONTable] = true
				}
				c := s.send(steps[:n])
				if prev != nil {
					samePrefix(t, "call "+itoa(n), prev, c.Body)
				}
				prev = c.Body
			}
			regs, _ := wire.ToolResults(w, prev)
			if strings.Contains(regs[0].Text, "lines folded") {
				t.Error("the first log was sent whole before log_window was on, so it stays whole")
			}
			if !strings.Contains(regs[2].Text, "lines folded") {
				t.Error("the diff arrived after log_window was enabled and should be windowed")
			}
			if strings.Contains(regs[4].Text, "]{") == false {
				t.Error("the JSON array arrived after json_table was enabled and should be a table")
			}
		})
	}
}

func TestObserveThenLosslessKeepsThePrefix(t *testing.T) {
	steps := agentSteps()
	h := trim.New(calm())
	s := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeObserve)
	var prev []byte
	for n := 1; n <= len(steps); n++ {
		if n == 4 {
			s.cfg.Mode = contract.TrimModeLossless
		}
		c := s.send(steps[:n])
		if prev != nil {
			samePrefix(t, "call "+itoa(n), prev, c.Body)
		}
		prev = c.Body
	}
	if r, _ := result(s.send(steps)); r.Action != contract.TrimActed {
		t.Errorf("after the switch the hook acts: %+v", r)
	}
}

// T3: a region the deadline stopped the hook from deciding is sent unchanged, and stays unchanged on every later call.
func TestDeadlineSkipIsSticky(t *testing.T) {
	steps := agentSteps()
	var skew atomic.Int64
	h := trim.New(trim.Options{Now: func() time.Time { return time.Now().Add(time.Duration(skew.Load())) }})
	s := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
	var prev []byte
	for n := 1; n <= 4; n++ {
		var c *tollgate.Call
		if n == 3 { // call 3 runs out of time before it decides its new region: its deadline is an hour away, the clock two ahead
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
			skew.Store(int64(2 * time.Hour))
			c = s.sendCtx(ctx, steps[:n])
			skew.Store(0)
			cancel()
		} else {
			c = s.send(steps[:n])
		}
		if prev != nil {
			samePrefix(t, "call "+itoa(n), prev, c.Body)
		}
		prev = c.Body
	}
	regs, _ := wire.ToolResults(tollgate.WireOpenAIChat, prev)
	if regs[2].Text != steps[2].result {
		t.Error("the region of the skipped call must stay unchanged")
	}
	if regs[0].Text == steps[0].result {
		t.Error("earlier regions were decided and stay trimmed")
	}
	if h.Status()["deadline_skips"].(int64) == 0 {
		t.Error("the skip is counted")
	}
}

func TestNeverTrimToolIsLeftAlone(t *testing.T) {
	steps := agentSteps()[:3]
	steps[0].tool = "Deploy_Logs"
	s := newSession(t, trim.New(calm()), tollgate.WireOpenAIChat, contract.TrimModeLossless)
	s.cfg.NeverTrimTools = []string{"deploy_logs"}
	c := s.send(steps)
	regs, _ := wire.ToolResults(tollgate.WireOpenAIChat, c.Body)
	if regs[0].Text != steps[0].result {
		t.Error("a never-trim tool's result was changed")
	}
	if regs[2].Text == steps[2].result {
		t.Error("other tools are still trimmed")
	}
}

// An Anthropic result flagged is_error is never touched; a tiny result is not worth analysing.
func TestErrorAndTinyResultsStay(t *testing.T) {
	steps := []step{{tool: "bash", args: `{"cmd":"x"}`, result: buildLog(), err: true}, {tool: "bash", args: `{"cmd":"y"}`, result: "ok"}, {tool: "bash", args: `{"cmd":"z"}`, result: buildLog()}}
	s := newSession(t, trim.New(calm()), tollgate.WireAnthropicMessages, contract.TrimModeLossless)
	regs, _ := wire.ToolResults(tollgate.WireAnthropicMessages, s.send(steps).Body)
	if len(regs) != 2 || regs[0].Text != "ok" || !strings.Contains(regs[1].Text, "lines folded") {
		t.Errorf("regions = %+v", regs)
	}
}

func TestSchemaTrimOnlyInAggressive(t *testing.T) {
	steps := agentSteps()[:1]
	for _, w := range wires {
		for _, mode := range []contract.TrimMode{contract.TrimModeLossless, contract.TrimModeAggressive} {
			s := newSession(t, trim.New(calm()), w, mode)
			s.long = true
			s.cfg.Stages[contract.StageSchemaTrim] = true
			c := s.send(steps)
			descs, err := wire.ToolDescriptions(w, c.Body)
			if err != nil || len(descs) != 1 {
				t.Fatal(descs, err)
			}
			cut := strings.HasSuffix(descs[0].Text, " …")
			if cut != (mode == contract.TrimModeAggressive) {
				t.Errorf("%s/%s: description cut = %v", w, mode, cut)
			}
			r, _ := result(c)
			hasSchema := false
			for _, sr := range r.Stages {
				hasSchema = hasSchema || sr.Stage == contract.StageSchemaTrim
			}
			if hasSchema != cut {
				t.Errorf("%s/%s: schema_trim stage reported = %v", w, mode, hasSchema)
			}
		}
	}
}

func TestWireErrorLeavesTheBodyAndIsCounted(t *testing.T) {
	h := trim.New(calm())
	s := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
	c := s.send(agentSteps()[:2])
	c.Body = []byte(`{"messages":[{"role":"tool","content":"unterminated`)
	bad := append([]byte(nil), c.Body...)
	c.Meta = map[string]any{}
	c.Session.Calls++
	if _, err := h.Before(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(c.Body, bad) || len(c.Meta) != 0 || h.Status()["edit_failed"] != int64(1) {
		t.Errorf("body %q meta %v status %v", c.Body, c.Meta, h.Status())
	}
}

func TestMemoIsBoundedAndForgetsClosedSessions(t *testing.T) {
	h := trim.New(trim.Options{MaxSessions: 1})
	a := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
	a.id = "a"
	a.send(agentSteps()[:2])
	b := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
	b.id = "b"
	b.send(agentSteps()[:2])
	st := h.Status()
	if st["memo_sessions"] != 1 || st["memo_evictions"].(int64) == 0 {
		t.Errorf("one session fits: %v", st)
	}
	h.OnSessionClosed(context.Background(), tollgate.Session{ID: "b", TenantID: "ws"})
	if st = h.Status(); st["memo_sessions"] != 0 || st["memo_bytes"] != int64(0) {
		t.Errorf("a closed session is forgotten: %v", st)
	}
	for _, k := range []string{"calls", "acted", "observed", "edit_failed", "deadline_skips", "unfolds", "memo_evictions"} {
		if _, ok := st[k]; !ok {
			t.Errorf("status lacks %q", k)
		}
	}
}

func TestRegionBudgetStopsAnalysisAndStaysUnchanged(t *testing.T) {
	h := trim.New(trim.Options{CallBytes: 4000})
	s := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
	steps := agentSteps()[:3]
	c := s.send(steps)
	regs, _ := wire.ToolResults(tollgate.WireOpenAIChat, c.Body)
	if regs[0].Text != steps[0].result || h.Status()["deadline_skips"].(int64) == 0 {
		t.Error("a region over the call's byte budget is left unchanged and counted")
	}
}

func TestConcurrentCallsOfOneSessionDecideOnce(t *testing.T) {
	h := trim.New(calm())
	steps := agentSteps()
	var wg sync.WaitGroup
	bodies := make([][]byte, 16)
	for i := range bodies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := newSession(t, h, tollgate.WireAnthropicMessages, contract.TrimModeLossless)
			s.calls = i
			bodies[i] = s.send(steps).Body
		}()
	}
	wg.Wait()
	for i := range bodies {
		if !bytes.Equal(bodies[i], bodies[0]) {
			t.Fatalf("call %d sent different bytes than call 0", i)
		}
	}
}

// The fold/unfold contract: when the agent repeats a call whose result was folded, the result is sent whole from then
// on, the monitors are told (Meta[trim.unfold]) and the repeat is not folded again.
func TestFoldThenUnfoldOnARepeatedCall(t *testing.T) {
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			h := trim.New(calm())
			s := newSession(t, h, w, contract.TrimModeLossless)
			mk := step{tool: "bash", args: `{"cmd":"make all"}`, result: buildLog()}
			steps := []step{mk}
			c1 := s.send(steps)
			regs, _ := wire.ToolResults(w, c1.Body)
			if !strings.Contains(regs[0].Text, "lines folded") {
				t.Fatal("the first result is folded")
			}
			if _, ok := c1.Meta[contract.MetaTrimUnfold]; ok {
				t.Error("nothing to unfold on call 1")
			}
			steps = append(steps, mk)
			c2 := s.send(steps)
			u, ok := c2.Meta[contract.MetaTrimUnfold].(contract.TrimUnfold)
			if !ok || len(u.ArgsHashes) != 1 || u.ArgsHashes[0] != contract.ToolArgsHash("bash", []byte(mk.args)) {
				t.Fatalf("unfold meta = %+v", c2.Meta[contract.MetaTrimUnfold])
			}
			regs, _ = wire.ToolResults(w, c2.Body)
			if regs[0].Text != mk.result || regs[1].Text != mk.result {
				t.Error("after the repeat both results are sent whole")
			}
			if r, _ := result(c2); r.Unfolded != 1 || r.Action != contract.TrimActed {
				t.Errorf("result = %+v", r)
			}
			steps = append(steps, mk)
			c3 := s.send(steps)
			if _, ok := c3.Meta[contract.MetaTrimUnfold]; ok {
				t.Error("only the first repeat is excused")
			}
			if regs, _ = wire.ToolResults(w, c3.Body); regs[0].Text != mk.result || regs[2].Text != mk.result {
				t.Error("the results stay whole")
			}
			if h.Status()["unfolds"] != int64(1) {
				t.Errorf("status = %v", h.Status())
			}
		})
	}
}

// Retrying the same request is not a repeat: it sends the same bytes and unfolds nothing.
func TestRetriedRequestIsNotARepeat(t *testing.T) {
	h := trim.New(calm())
	s := newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
	steps := agentSteps()[:3]
	a, b := s.send(steps), s.send(steps)
	if !bytes.Equal(a.Body, b.Body) {
		t.Error("a retry sent other bytes")
	}
	if _, ok := b.Meta[contract.MetaTrimUnfold]; ok || h.Status()["unfolds"] != int64(0) {
		t.Error("a retry unfolded something")
	}
}

// A different tool call is not a repeat even when its output is identical: the second result is collapsed, not unfolded.
func TestIdenticalOutputOfAnotherCallIsCollapsed(t *testing.T) {
	steps := agentSteps()[:4] // steps 1 and 3 run different commands with the same output
	s := newSession(t, trim.New(calm()), tollgate.WireOpenAIChat, contract.TrimModeLossless)
	c := s.send(steps)
	regs, _ := wire.ToolResults(tollgate.WireOpenAIChat, c.Body)
	if !strings.HasPrefix(regs[3].Text, "[unchanged since step") {
		t.Errorf("the repeated output = %q", regs[3].Text)
	}
	if _, ok := c.Meta[contract.MetaTrimUnfold]; ok {
		t.Error("no unfold for a different call")
	}
}
