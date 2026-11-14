package memory_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/memory"
	"github.com/ethosagent/meterwise/internal/store"
)

const chatBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"fix the parser"}]}`

func attr(mode contract.MemoryMode, maxHints int) string {
	c := contract.DefaultMemoryConfig()
	c.Mode, c.MaxHintsPerSession = mode, maxHints
	return c.Attr()
}

func msgs() []tollgate.Message {
	return []tollgate.Message{
		{Role: tollgate.RoleUser, Text: "fix the parser bug, key sk-abcdefghijkl1234"},
		{Role: tollgate.RoleAssistant, ToolCalls: []tollgate.ToolCall{{ID: "1", Name: "edit_file", Arguments: json.RawMessage(`{"path":"lex/a.go"}`)}}},
		{Role: tollgate.RoleTool, ToolCallID: "1", Text: "ok\nerror: undefined: lexer.Next\n"},
	}
}

func callN(n int, a string, capture bool) *tollgate.Call {
	return &tollgate.Call{
		ID: "c", Tenant: tollgate.Tenant{ID: ws, Attrs: map[string]string{memory.AttrMemory: a}, Policy: &tollgate.Policy{Capture: capture}},
		Session:  tollgate.Session{ID: "s1", TenantID: ws, Calls: n},
		Provider: tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat}, Model: "gpt-4o", Body: []byte(chatBody), Messages: msgs(),
		Meta: map[string]any{},
	}
}

func cand(sim float64) store.Candidate {
	return store.Candidate{ID: 7, SessionID: "old", Approach: "read the lexer first.", Fix: "guard the nil token", Similarity: sim,
		Signature: contract.TraceSignature{Files: []string{"lex/a.go"}}}
}

// afterOne runs the hook's After of call 1 and waits until the recall has produced (or declined) its result.
func afterOne(t *testing.T, r *rig, a string) {
	t.Helper()
	c := callN(1, a, true)
	r.hook.After(t.Context(), c, tollgate.Metric{})
	eventually(t, "the recall job", r.recalled)
}

func (f *fakeStore) nearestCount() int { f.mu.Lock(); defer f.mu.Unlock(); return f.nearest }

func (r *rig) recalled() bool {
	s, _ := r.e.Source().Status()["jobs"].(map[string]any)["recall"].(map[string]any)
	return s["done"].(int64)+s["failed"].(int64) >= 1
}

func TestHintLandsOnCallTwoAsOneLine(t *testing.T) {
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.9)}
	a := attr(contract.MemoryModeHint, 1)
	// Call 1 has nothing cached yet: Before adds no hint and no I/O.
	c1 := callN(1, a, true)
	if _, err := r.hook.Before(t.Context(), c1); err != nil || strings.Contains(string(c1.Body), memory.HintPrefix) || c1.Meta[contract.MetaMemory] != nil {
		t.Fatalf("call 1 must pass untouched: %v %s %v", err, c1.Body, c1.Meta)
	}
	afterOne(t, r, a)
	c2 := callN(2, a, true)
	if resp, err := r.hook.Before(t.Context(), c2); resp != nil || err != nil {
		t.Fatalf("Before = %v, %v", resp, err)
	}
	if n := strings.Count(string(c2.Body), memory.HintPrefix); n != 1 {
		t.Fatalf("hint lines in the body = %d, want exactly 1: %s", n, c2.Body)
	}
	var doc map[string]any
	if err := json.Unmarshal(c2.Body, &doc); err != nil {
		t.Fatalf("the edited body is not JSON: %v", err)
	}
	mh, ok := c2.Meta[contract.MetaMemory].(contract.MemoryHint)
	if !ok || !mh.Injected || mh.TraceID != 7 || mh.SimilarityBp != 9000 || mh.Mode != contract.MemoryModeHint {
		t.Fatalf("Meta[memory] = %#v", c2.Meta[contract.MetaMemory])
	}
	// The one hint per session is spent: call 3 is untouched.
	c3 := callN(3, a, true)
	_, _ = r.hook.Before(t.Context(), c3)
	if string(c3.Body) != chatBody || c3.Meta[contract.MetaMemory] != nil {
		t.Fatalf("call 3 got a second hint: %s", c3.Body)
	}
}

func TestObserveRecordsTheHitWithoutInjecting(t *testing.T) {
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.95)}
	a := attr(contract.MemoryModeObserve, 1)
	afterOne(t, r, a)
	c2 := callN(2, a, true)
	_, _ = r.hook.Before(t.Context(), c2)
	if string(c2.Body) != chatBody {
		t.Fatalf("observe mode changed the body: %s", c2.Body)
	}
	if mh, ok := c2.Meta[contract.MetaMemory].(contract.MemoryHint); !ok || mh.Injected || mh.Mode != contract.MemoryModeObserve || mh.TraceID != 7 {
		t.Fatalf("Meta[memory] = %#v", c2.Meta[contract.MetaMemory])
	}
}

func TestBelowThresholdGivesNoHint(t *testing.T) {
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.70)}
	a := attr(contract.MemoryModeHint, 1)
	afterOne(t, r, a)
	c2 := callN(2, a, true)
	_, _ = r.hook.Before(t.Context(), c2)
	if string(c2.Body) != chatBody || c2.Meta[contract.MetaMemory] != nil {
		t.Fatalf("a 0.70 match is under the 0.82 threshold: %s %v", c2.Body, c2.Meta)
	}
}

func TestPrivacyAndOffRunNothing(t *testing.T) {
	for name, tc := range map[string]struct {
		attr    string
		capture bool
		mut     func(*memory.Options)
	}{
		"capture off for the workspace": {attr(contract.MemoryModeHint, 1), false, nil},
		"mode off":                      {attr(contract.MemoryModeOff, 1), true, nil},
		"global privacy mode on":        {attr(contract.MemoryModeHint, 1), true, func(o *memory.Options) { on := true; o.PrivacyMode = &on }},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, tc.mut)
			r.st.cands = []store.Candidate{cand(0.99)}
			c := callN(1, tc.attr, tc.capture)
			_, _ = r.hook.Before(t.Context(), c)
			r.hook.After(t.Context(), c, tollgate.Metric{})
			c.Session.Calls = 2
			_, _ = r.hook.Before(t.Context(), c)
			if r.emb.count() != 0 || r.st.nearestCount() != 0 || string(c.Body) != chatBody {
				t.Fatalf("memory ran: embeds=%d nearest=%d body=%s", r.emb.count(), r.st.nearestCount(), c.Body)
			}
		})
	}
}

func TestNoKeyMeansNoHook(t *testing.T) {
	r := newRig(t, func(o *memory.Options) { o.Distiller, o.Embedder = nil, nil })
	if r.hook != nil {
		t.Fatal("an engine without a key must not offer a hook")
	}
	enabled, reason, err := r.e.API().Status(t.Context(), ws)
	if enabled || reason != contract.MemoryDisabledNoKey || err != nil {
		t.Fatalf("Status = %v %q %v", enabled, reason, err)
	}
}

func TestBudgetPausesRecallAndReportsIt(t *testing.T) {
	r := newRig(t, nil)
	r.st.spent = 1_000_000 // the whole daily budget
	r.st.cands = []store.Candidate{cand(0.99)}
	a := attr(contract.MemoryModeHint, 1)
	r.hook.After(t.Context(), callN(1, a, true), tollgate.Metric{})
	eventually(t, "the recall job", r.recalled)
	if r.emb.count() != 0 || r.st.nearestCount() != 0 {
		t.Fatalf("a paused budget still called the model: embeds=%d", r.emb.count())
	}
	if got := r.e.Source().Status()["skipped_budget"].(int64); got != 1 {
		t.Fatalf("skipped_budget = %d", got)
	}
	if enabled, reason, err := r.e.API().Status(t.Context(), ws); enabled || reason != contract.MemoryDisabledBudget || err != nil {
		t.Fatalf("Status = %v %q %v, want disabled for budget", enabled, reason, err)
	}
	if st, err := r.e.API().Stats(t.Context(), ws, contract.SavingsRange{}); err == nil && !st.BudgetPaused {
		t.Fatal("Stats must report budgetPaused")
	}
}

func TestForgetDropsWaitingCandidates(t *testing.T) {
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.9)}
	a := attr(contract.MemoryModeHint, 1)
	afterOne(t, r, a)
	if n, err := r.e.API().Forget(t.Context(), ws); n != 3 || err != nil {
		t.Fatalf("Forget = %d, %v", n, err)
	}
	c2 := callN(2, a, true)
	_, _ = r.hook.Before(t.Context(), c2)
	if string(c2.Body) != chatBody {
		t.Fatalf("a forgotten trace was still injected: %s", c2.Body)
	}
}

func TestInjectedTraceIsTouchedOffThePath(t *testing.T) {
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.9)}
	a := attr(contract.MemoryModeHint, 1)
	afterOne(t, r, a)
	c2 := callN(2, a, true)
	_, _ = r.hook.Before(t.Context(), c2)
	r.hook.After(t.Context(), c2, tollgate.Metric{})
	eventually(t, "the touch", func() bool {
		r.st.mu.Lock()
		defer r.st.mu.Unlock()
		return len(r.st.touched) == 1 && r.st.touched[0] == 7
	})
}
