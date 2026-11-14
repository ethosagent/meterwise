package memory_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/memory"
	"github.com/ethosagent/meterwise/internal/store"
)

// apiStore adds the reads and the recount the control-plane port makes.
type apiStore struct {
	*fakeStore
	mu       sync.Mutex
	hits     []store.HitRow
	recount  []int64
	captured bool
}

func (a *apiStore) SessionMemoryHits(context.Context, string, string) ([]store.HitRow, error) {
	return a.hits, nil
}

func (a *apiStore) MemoryHit(_ context.Context, _ string, id int64) (store.HitRow, error) {
	for _, h := range a.hits {
		if h.ID == id {
			return h, nil
		}
	}
	return store.HitRow{}, contract.NewError(contract.CodeNotFound, "no such hit")
}

func (a *apiStore) RecountTraceVotes(_ context.Context, _ string, id int64) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.recount = append(a.recount, id)
	return 6000, nil
}

func (a *apiStore) WorkspaceCaptureAllowed(context.Context, string) (bool, error) {
	return a.captured, nil
}

// fbLedger records the feedback it is asked to set and names trace 7 as the hit's trace.
type fbLedger struct {
	fakeLedger
	set []string
}

func (l *fbLedger) SetHitFeedback(_ context.Context, _ string, _ int64, fb *string) (store.HitRef, error) {
	if fb != nil {
		l.set = append(l.set, *fb)
	}
	return store.HitRef{HitID: 1, TraceID: 7}, nil
}

func apiRig(t *testing.T, mut func(*memory.Options)) (*memory.API, *apiStore, *fbLedger) {
	t.Helper()
	st := &apiStore{fakeStore: &fakeStore{}, captured: true}
	led := &fbLedger{}
	r := newRig(t, func(o *memory.Options) {
		o.Store, o.Ledger = st, led
		if mut != nil {
			mut(o)
		}
	})
	return r.e.API(), st, led
}

func TestStatusSaysWhyMemoryIsNotWorking(t *testing.T) {
	for _, c := range []struct {
		name  string
		mut   func(*memory.Options)
		cap   bool
		spent contract.MicroUSD
		ok    bool
		why   string
	}{
		{"working", nil, true, 0, true, ""},
		{"no key", func(o *memory.Options) { o.Distiller, o.Embedder = nil, nil }, true, 0, false, contract.MemoryDisabledNoKey},
		{"global privacy", func(o *memory.Options) { on := true; o.PrivacyMode = &on }, true, 0, false, contract.MemoryDisabledPrivacy},
		{"workspace privacy", nil, false, 0, false, contract.MemoryDisabledPrivacy},
		{"budget", nil, true, 2_000_000, false, contract.MemoryDisabledBudget},
	} {
		t.Run(c.name, func(t *testing.T) {
			api, st, _ := apiRig(t, c.mut)
			st.captured, st.spent = c.cap, c.spent
			ok, why, err := api.Status(t.Context(), ws)
			if err != nil || ok != c.ok || why != c.why {
				t.Fatalf("Status = %v %q %v, want %v %q", ok, why, err, c.ok, c.why)
			}
		})
	}
}

func TestSessionHitsRenderTheHintFromTheTrace(t *testing.T) {
	api, st, _ := apiRig(t, nil)
	fb := "helpful"
	st.hits = []store.HitRow{
		{ID: 1, CallID: "c1", TraceID: 7, SimilarityBp: 9100, Mode: contract.MemoryModeHint, Injected: true, Feedback: &fb, SourceSessionID: "old",
			Approach: "read the lexer first.", Fix: "guard the nil token", Files: []string{"lex/a.go"}},
		{ID: 2, CallID: "c2", TraceID: 8, SimilarityBp: 8400, Mode: contract.MemoryModeObserve},
	}
	got, err := api.SessionHits(context.Background(), ws, "s1")
	if err != nil || len(got) != 2 {
		t.Fatalf("hits = %+v %v", got, err)
	}
	if got[0].ID != "1" || got[0].TraceID != "7" || !got[0].Injected || got[0].Feedback == nil || *got[0].Feedback != "helpful" ||
		!strings.Contains(got[0].Hint, "guard the nil token") || strings.Contains(got[0].Hint, "\n") {
		t.Errorf("injected hit = %+v", got[0])
	}
	if got[1].Injected || got[1].Hint != "" {
		t.Errorf("an observed hit carries no hint text: %+v", got[1])
	}
}

func TestFeedbackGoesThroughTheLedgerThenTheVotesAreRecounted(t *testing.T) {
	api, st, led := apiRig(t, nil)
	st.hits = []store.HitRow{{ID: 1, CallID: "c1", TraceID: 7, Mode: contract.MemoryModeHint, Injected: true}}
	up := "helpful"
	if _, err := api.SetHitFeedback(context.Background(), ws, "1", &up); err != nil {
		t.Fatal(err)
	}
	if len(led.set) != 1 || led.set[0] != "helpful" {
		t.Errorf("the ledger was asked for %v", led.set)
	}
	if len(st.recount) != 1 || st.recount[0] != 7 {
		t.Errorf("votes recounted for %v, want trace 7", st.recount)
	}
	for _, bad := range []string{"", "abc", "0", "-3"} {
		if _, err := api.SetHitFeedback(context.Background(), ws, bad, &up); err == nil {
			t.Errorf("hit id %q must be refused", bad)
		}
	}
}
