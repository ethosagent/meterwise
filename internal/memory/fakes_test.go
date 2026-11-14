package memory_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/memory"
	"github.com/ethosagent/meterwise/internal/store"
)

const ws = "AAAAAAAAAAAAAAAAAAAAAA"

// fakeStore implements only what a test sets; the embedded nil interface panics on anything else, so an unexpected
// call fails loudly.
type fakeStore struct {
	memory.Store
	mu         sync.Mutex
	cands      []store.Candidate
	nearestErr error
	traces     []store.NewTrace
	failures   []store.TraceFailure
	usage      []string
	spent      contract.MicroUSD
	uncredited []int64
	baseline   store.Baseline
	touched    []int64
	nearest    int
	hold       chan struct{} // when set, NearestTraces waits for it to close (a slow recall)
	noCapture  bool          // the workspace has switched capture off
}

func (f *fakeStore) NearestTraces(_ context.Context, _ string, _ []float32, _ int, _ float32) ([]store.Candidate, error) {
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nearest++
	return f.cands, f.nearestErr
}

func (f *fakeStore) InsertTrace(_ context.Context, t store.NewTrace) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.traces = append(f.traces, t)
	return int64(len(f.traces)), true, nil
}

func (f *fakeStore) InsertTraceFailure(_ context.Context, x store.TraceFailure) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, x)
	return nil
}

func (f *fakeStore) AddSelfUsage(_ context.Context, _, kind, model string, in, out int64, cost contract.MicroUSD) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.usage = append(f.usage, fmt.Sprintf("%s/%s/%d/%d/%d", kind, model, in, out, cost))
	return nil
}

func (f *fakeStore) SelfUsageSince(context.Context, string, time.Time) (contract.MicroUSD, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.spent, nil
}

func (f *fakeStore) TouchTrace(_ context.Context, _ string, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.touched = append(f.touched, id)
	return nil
}

func (f *fakeStore) UncreditedHits(context.Context, string, string) ([]int64, error) {
	return f.uncredited, nil
}

func (f *fakeStore) SessionBaseline(context.Context, string, string, time.Time) (store.Baseline, error) {
	return f.baseline, nil
}

func (f *fakeStore) ForgetTraces(context.Context, string) (int64, error) { return 3, nil }

type fakeEmbedder struct {
	mu    sync.Mutex
	texts []string
	err   error // when set, every embedding fails
}

func (e *fakeEmbedder) Embed(_ context.Context, _, text string) ([]float32, memory.Usage, error) {
	e.mu.Lock()
	e.texts = append(e.texts, text)
	e.mu.Unlock()
	if e.err != nil {
		return nil, memory.Usage{}, e.err
	}
	v := make([]float32, store.EmbedDim)
	v[0] = 1
	return v, memory.Usage{Input: 10}, nil
}

func (e *fakeEmbedder) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.texts)
}

type fakeDistiller struct {
	mu   sync.Mutex
	sums []memory.Summary
	out  memory.Trace
	err  error
}

func (d *fakeDistiller) Distil(_ context.Context, _ string, s memory.Summary) (memory.Trace, memory.Usage, error) {
	d.mu.Lock()
	d.sums = append(d.sums, s)
	d.mu.Unlock()
	return d.out, memory.Usage{Input: 100, Output: 50}, d.err
}

type credit struct {
	hit, tokens int64
	usd         contract.MicroUSD
}

type fakeLedger struct {
	mu      sync.Mutex
	credits []credit
}

func (l *fakeLedger) SetHitFeedback(context.Context, string, int64, *string) (store.HitRef, error) {
	return store.HitRef{}, nil
}

func (l *fakeLedger) CreditMemory(_ context.Context, _, _ string, hit, tokens int64, usd contract.MicroUSD) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.credits = append(l.credits, credit{hit, tokens, usd})
	return nil
}

func (l *fakeLedger) got() []credit {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]credit(nil), l.credits...)
}

// rig is an engine over fakes.
type rig struct {
	e    *memory.Engine
	st   *fakeStore
	emb  *fakeEmbedder
	dis  *fakeDistiller
	led  *fakeLedger
	hook tollgate.Hook
}

func newRig(t *testing.T, mut func(*memory.Options)) *rig {
	t.Helper()
	on := false
	r := &rig{st: &fakeStore{}, emb: &fakeEmbedder{}, dis: &fakeDistiller{}, led: &fakeLedger{}}
	r.dis.out = memory.Trace{TaskSummary: "fix the parser", Approach: "read the lexer", Fix: "guard the nil", Confidence: 0.9}
	o := memory.Options{Store: r.st, Ledger: r.led, Distiller: r.dis, Embedder: r.emb, Model: "m", EmbedModel: "e", SelfBudget: 1_000_000, PrivacyMode: &on}
	if mut != nil {
		mut(&o)
	}
	r.e = memory.New(t.Context(), o)
	t.Cleanup(r.e.Close)
	r.hook = r.e.Hook()
	return r
}

// eventually polls cond for up to two seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 400 {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (f *fakeStore) WorkspaceCaptureAllowed(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.noCapture, nil
}

func (f *fakeStore) MemoryStats(context.Context, string, contract.SavingsRange, time.Time) (contract.MemoryStats, error) {
	return contract.MemoryStats{}, nil
}
