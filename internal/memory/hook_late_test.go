package memory_test

import (
	"errors"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

func (r *rig) jobCount(kind, field string) int64 {
	m, _ := r.e.Source().Status()["jobs"].(map[string]any)[kind].(map[string]any)
	n, _ := m[field].(int64)
	return n
}

// A recall that finishes after call 3 gives no hint on call 4 or later: the moment has passed (plan 09 3.2).
func TestASlowRecallThatFinishesAfterCallThreeGivesNoHint(t *testing.T) {
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.95)}
	r.st.hold = make(chan struct{})
	a := attr(contract.MemoryModeHint, 1)
	r.hook.After(t.Context(), callN(1, a, true), tollgate.Metric{}) // the recall starts and blocks in the store
	for n := 2; n <= 3; n++ {
		c := callN(n, a, true)
		if _, err := r.hook.Before(t.Context(), c); err != nil || string(c.Body) != chatBody {
			t.Fatalf("call %d with the recall still running must pass untouched: %v %s", n, err, c.Body)
		}
	}
	close(r.st.hold)
	eventually(t, "the slow recall", r.recalled)
	c4 := callN(4, a, true)
	if _, err := r.hook.Before(t.Context(), c4); err != nil || string(c4.Body) != chatBody || c4.Meta[contract.MetaMemory] != nil {
		t.Fatalf("call 4 got a late hint: %v %s %v", err, c4.Body, c4.Meta)
	}
	c5 := callN(5, a, true)
	_, _ = r.hook.Before(t.Context(), c5)
	if string(c5.Body) != chatBody {
		t.Fatalf("a dropped hint came back on call 5: %s", c5.Body)
	}
}

// A failing embedding or nearest query is counted as a failed job, leaves no candidate, and never reaches the client.
func TestRecallFailuresAreCountedAndNeverReachTheClient(t *testing.T) {
	for name, tc := range map[string]struct {
		mut func(*rig)
	}{
		"embedding fails": {func(r *rig) { r.emb.err = errors.New("embed 503") }},
		"nearest fails":   {func(r *rig) { r.st.nearestErr = errors.New("db down") }},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			r.st.cands = []store.Candidate{cand(0.95)}
			tc.mut(r)
			a := attr(contract.MemoryModeHint, 1)
			c1 := callN(1, a, true)
			r.hook.After(t.Context(), c1, tollgate.Metric{}) // returns at once, whatever the job does
			eventually(t, "the failed job", func() bool { return r.jobCount("recall", "failed") == 1 })
			if r.jobCount("recall", "done") != 0 {
				t.Errorf("a failed recall counted as done")
			}
			c2 := callN(2, a, true)
			resp, err := r.hook.Before(t.Context(), c2)
			if resp != nil || err != nil || string(c2.Body) != chatBody || c2.Meta[contract.MetaMemory] != nil {
				t.Fatalf("the client saw the failure: %v %v %s %v", resp, err, c2.Body, c2.Meta)
			}
		})
	}
}

// A hint whose edit failed was not sent: it is neither an injected nor an observed hit.
func TestAHintThatCouldNotBeSplicedIsNotARecordedHit(t *testing.T) {
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.95)}
	a := attr(contract.MemoryModeHint, 1)
	afterOne(t, r, a)
	c2 := callN(2, a, true)
	c2.Body = []byte(`{not json`)
	if _, err := r.hook.Before(t.Context(), c2); err != nil {
		t.Fatal(err)
	}
	st := r.e.Source().Status()
	if c2.Meta[contract.MetaMemory] != nil || st["edit_failed"] != int64(1) || st["hits_observed"] != int64(0) || st["hints_injected"] != int64(0) {
		t.Fatalf("meta %v status %v", c2.Meta[contract.MetaMemory], st)
	}
}

// Capture switched off between the calls and the close: nothing is stored for the session.
func TestCaptureSwitchedOffBeforeTheCloseStoresNothing(t *testing.T) {
	r := newRig(t, nil)
	a := attr(contract.MemoryModeObserve, 1)
	r.hook.After(t.Context(), callN(1, a, true), tollgate.Metric{})
	eventually(t, "the recall", r.recalled)
	r.st.mu.Lock()
	r.st.noCapture = true
	r.st.mu.Unlock()
	r.e.OnClosed(t.Context(), ws, "s1", contract.OutcomeCompleted, 5)
	eventually(t, "the distil job", func() bool { return r.jobCount("distil", "done") == 1 })
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	if len(r.st.traces) != 0 || len(r.st.failures) != 0 || len(r.dis.sums) != 0 {
		t.Fatalf("stored after privacy was switched on: %d traces, %d failures, %d distil calls", len(r.st.traces), len(r.st.failures), len(r.dis.sums))
	}
	if got := r.e.Source().Status()["skipped_privacy"]; got != int64(1) {
		t.Errorf("skipped_privacy = %v, want 1", got)
	}
}

func TestStatusShowsTheBudgetState(t *testing.T) {
	r := newRig(t, nil)
	r.st.spent = 2_000_000
	a := attr(contract.MemoryModeObserve, 1)
	r.hook.After(t.Context(), callN(1, a, true), tollgate.Metric{})
	eventually(t, "the budget read", func() bool { return r.e.Source().Status()["budget_paused_workspaces"] == 1 })
	st := r.e.Source().Status()
	if st["budget_cap_micro_usd"] != int64(1_000_000) || st["budget_spent_micro_usd"] != int64(2_000_000) {
		t.Fatalf("budget block = %v / %v", st["budget_cap_micro_usd"], st["budget_spent_micro_usd"])
	}
}
