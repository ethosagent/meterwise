package memory_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/memory"
	"github.com/ethosagent/meterwise/internal/store"
)

// session runs n calls of a session through After so the engine holds its summary.
func session(t *testing.T, r *rig, a string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		c := callN(i, a, true)
		_, _ = r.hook.Before(t.Context(), c)
		r.hook.After(t.Context(), c, tollgate.Metric{})
	}
	eventually(t, "the first recall", r.recalled)
}

func jobDone(r *rig, kind string) int64 {
	s := r.e.Source().Status()["jobs"].(map[string]any)[kind].(map[string]any)
	return s["done"].(int64) + s["failed"].(int64)
}

func TestCompletedSessionWithAWriteIsDistilledAndRedacted(t *testing.T) {
	r := newRig(t, nil)
	r.dis.out = memory.Trace{TaskSummary: "fix the parser, key sk-ant-abcdef123456", Observations: []string{"Bearer abcdef123456 was logged"},
		Approach: "read the lexer", Fix: "guard the nil token", Confidence: 0.8, Signature: contract.TraceSignature{Files: []string{"lex/a.go"}}}
	session(t, r, attr(contract.MemoryModeHint, 1), 4)
	r.e.OnClosed(t.Context(), ws, "s1", contract.OutcomeCompleted, 5)
	eventually(t, "the distil job", func() bool { return jobDone(r, "distil") == 1 })

	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	if len(r.st.traces) != 1 || len(r.st.failures) != 0 {
		t.Fatalf("traces=%d failures=%d", len(r.st.traces), len(r.st.failures))
	}
	tr := r.st.traces[0]
	if tr.WorkspaceID != ws || tr.SessionID != "s1" || tr.PromptV != memory.PromptVersion || tr.EmbedModel != "e" || len(tr.Embedding) != store.EmbedDim {
		t.Fatalf("trace = %+v", tr)
	}
	if text := tr.TaskSummary + strings.Join(tr.Observations, " "); strings.Contains(text, "sk-ant") || strings.Contains(text, "abcdef123456") {
		t.Fatalf("a credential reached the stored trace: %q", text)
	}
	// What the distiller was shown: a short summary, redacted, never the raw conversation.
	sum := r.dis.sums[0]
	if strings.Contains(sum.Task, "sk-abcdefghijkl1234") || len(sum.Task) > 200 || len(sum.Tools) != 1 || sum.Tools[0] != "edit_file" ||
		len(sum.Files) != 1 || sum.Files[0] != "lex/a.go" || len(sum.Errors) != 1 || !strings.Contains(sum.Errors[0], "undefined") {
		t.Fatalf("summary = %+v", sum)
	}
	if len(r.st.usage) != 3 || !strings.HasPrefix(r.st.usage[0], "embed/e/10/0/") || !strings.HasPrefix(r.st.usage[1], "distil/m/100/50/") || !strings.HasPrefix(r.st.usage[2], "embed/e/10/0/") {
		t.Fatalf("self usage = %v (recall embed, distil, trace embed)", r.st.usage)
	}
}

func TestShortOrAbandonedSessionLeavesOnlyAFailureRecord(t *testing.T) {
	for name, tc := range map[string]struct {
		outcome string
		calls   int
	}{"abandoned": {contract.OutcomeAbandoned, 6}, "too short": {contract.OutcomeCompleted, 3}} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, nil)
			session(t, r, attr(contract.MemoryModeHint, 1), 3)
			r.e.OnClosed(t.Context(), ws, "s1", tc.outcome, tc.calls)
			eventually(t, "the failure record", func() bool { return jobDone(r, "failure") == 1 })
			r.st.mu.Lock()
			defer r.st.mu.Unlock()
			if len(r.st.traces) != 0 || len(r.st.failures) != 1 || r.st.failures[0].Calls != tc.calls || !strings.Contains(r.st.failures[0].LastError, "undefined") {
				t.Fatalf("traces=%d failures=%+v", len(r.st.traces), r.st.failures)
			}
			if len(r.dis.sums) != 0 {
				t.Fatal("no distil call for a session that did not end in a solution")
			}
		})
	}
}

func TestDistilFailureStoresNoTrace(t *testing.T) {
	r := newRig(t, nil)
	r.dis.err = errors.New("model down")
	session(t, r, attr(contract.MemoryModeHint, 1), 4)
	r.e.OnClosed(t.Context(), ws, "s1", contract.OutcomeCompleted, 5)
	eventually(t, "the distil job", func() bool { return jobDone(r, "distil") == 1 })
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	if len(r.st.traces) != 0 || len(r.st.failures) != 1 {
		t.Fatalf("traces=%d failures=%d", len(r.st.traces), len(r.st.failures))
	}
}

func TestCaptureOffMeansNothingIsCaptured(t *testing.T) {
	r := newRig(t, nil)
	a := attr(contract.MemoryModeHint, 1)
	c := callN(1, a, false) // capture off for the workspace
	_, _ = r.hook.Before(t.Context(), c)
	r.hook.After(t.Context(), c, tollgate.Metric{})
	r.e.OnClosed(t.Context(), ws, "s1", contract.OutcomeCompleted, 6)
	if got := r.e.Source().Status()["closed_without_state"].(int64); got != 1 {
		t.Fatalf("closed_without_state = %d: a private session must leave no summary", got)
	}
	if len(r.dis.sums) != 0 {
		t.Fatal("distilled a private session")
	}
}

func TestLikelyCreditIsOnceAndBoundedByTheBaseline(t *testing.T) {
	// Baseline: 4 completed sessions, median 10 calls; 40 calls, 40000 tokens, 4 USD in total (an average call is 100 000 micro-USD
	// and 1000 tokens). This session took 6 calls: 4 fewer, so 400 000 micro-USD and 4000 tokens, under one average session.
	r := newRig(t, nil)
	r.st.uncredited = []int64{11, 12}
	r.st.baseline = store.Baseline{Sessions: 4, MedianCalls: 10, Calls: 40, Tokens: 40000, Cost: 4_000_000}
	r.e.OnClosed(t.Context(), ws, "s1", contract.OutcomeCompleted, 6)
	eventually(t, "the credit", func() bool { return len(r.led.got()) == 1 })
	if got := r.led.got()[0]; got != (credit{hit: 11, tokens: 4000, usd: 400_000}) {
		t.Fatalf("credit = %+v", got)
	}

	// A session as long as the median, a thin baseline and an abandoned close credit nothing.
	for name, tc := range map[string]struct {
		calls   int
		outcome string
		base    store.Baseline
	}{
		"as long as usual":  {10, contract.OutcomeCompleted, r.st.baseline},
		"thin baseline":     {2, contract.OutcomeCompleted, store.Baseline{Sessions: 2, MedianCalls: 10, Calls: 20, Tokens: 2000, Cost: 1000}},
		"abandoned session": {2, contract.OutcomeAbandoned, r.st.baseline},
	} {
		r2 := newRig(t, nil)
		r2.st.uncredited, r2.st.baseline = []int64{1}, tc.base
		r2.e.OnClosed(t.Context(), ws, "s9", tc.outcome, tc.calls)
		eventually(t, name, func() bool { return tc.outcome == contract.OutcomeAbandoned || jobDone(r2, "credit") == 1 })
		if n := len(r2.led.got()); n != 0 {
			t.Fatalf("%s: credited %d times", name, n)
		}
	}
}

func TestCreditNeverExceedsOneAverageSession(t *testing.T) {
	r := newRig(t, nil)
	r.st.uncredited = []int64{5}
	// Median 100 calls against a 1 call session: 99 fewer calls would be 990 000 000, but a session costs 10 000 000 on average.
	r.st.baseline = store.Baseline{Sessions: 4, MedianCalls: 100, Calls: 40, Tokens: 40000, Cost: 40_000_000}
	r.e.OnClosed(t.Context(), ws, "s1", contract.OutcomeCompleted, 1)
	eventually(t, "the credit", func() bool { return len(r.led.got()) == 1 })
	if got := r.led.got()[0]; got.usd != 10_000_000 || got.tokens != 10_000 {
		t.Fatalf("credit = %+v, want the average session", got)
	}
}

// meet is a Distiller and an Embedder that, once armed, only answer when the other one is in flight too.
type meet struct {
	armed   atomic.Bool
	arrived atomic.Int32
	both    chan struct{}
}

func (m *meet) wait() error {
	if !m.armed.Load() {
		return nil
	}
	if m.arrived.Add(1) == 2 {
		close(m.both)
	}
	select {
	case <-m.both:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("the other model call never started")
	}
}

type meetEmbedder struct {
	*meet
	memory.Embedder
}

func (m meetEmbedder) Embed(ctx context.Context, model, text string) ([]float32, memory.Usage, error) {
	if err := m.wait(); err != nil {
		return nil, memory.Usage{}, err
	}
	return m.Embedder.Embed(ctx, model, text)
}

type meetDistiller struct {
	*meet
	memory.Distiller
}

func (m meetDistiller) Distil(ctx context.Context, model string, s memory.Summary) (memory.Trace, memory.Usage, error) {
	if err := m.wait(); err != nil {
		return memory.Trace{}, memory.Usage{}, err
	}
	return m.Distiller.Distil(ctx, model, s)
}

func TestDistilAndEmbedRunTogether(t *testing.T) {
	m := &meet{both: make(chan struct{})}
	good := memory.Trace{TaskSummary: "fix", Approach: "read", Fix: "guard", Confidence: 0.9}
	r := newRig(t, func(o *memory.Options) {
		o.Embedder = meetEmbedder{m, &fakeEmbedder{}}
		o.Distiller = meetDistiller{m, &fakeDistiller{out: good}}
	})
	session(t, r, attr(contract.MemoryModeHint, 1), 4)
	m.armed.Store(true)
	r.e.OnClosed(t.Context(), ws, "s1", contract.OutcomeCompleted, 5)
	eventually(t, "the distil job", func() bool { return jobDone(r, "distil") == 1 })
	r.st.mu.Lock()
	defer r.st.mu.Unlock()
	if len(r.st.traces) != 1 || len(r.st.failures) != 0 {
		t.Fatalf("traces=%d failures=%d: distil and embed did not overlap", len(r.st.traces), len(r.st.failures))
	}
	if n := len(r.st.usage); n != 3 || !strings.HasPrefix(r.st.usage[1], "distil/") || !strings.HasPrefix(r.st.usage[2], "embed/") {
		t.Fatalf("self usage = %v, want the distil row before the embed row", r.st.usage)
	}
}
