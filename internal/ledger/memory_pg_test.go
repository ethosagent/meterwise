package ledger_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/store"
)

// memCall is a completed call of workspace ws whose memory hook reported mh (nil: no memory on the call).
func memCall(id, ws string, mh *contract.MemoryHint) *tollgate.Call {
	c := directCall(id, "fix the parser", nil, 1)
	c.Tenant.ID, c.Tenant.Attrs = ws, map[string]string{"org_id": store.OrgID()}
	c.Session.TenantID = ws
	if mh != nil {
		c.Meta[contract.MetaMemory] = *mh
	}
	return c
}

func TestMemoryHitsAreWrittenOncePerCallAndCreditedAsLikelyNeverCertain(t *testing.T) {
	s, ws := pgStore(t)
	h := newHook(t, s, nil)
	var mu sync.Mutex
	var closed []ledger.ClosedSession
	h.SetOnClosed(func(c ledger.ClosedSession) { mu.Lock(); closed = append(closed, c); mu.Unlock() })
	ctx := context.Background()

	complete(t, h, memCall("c1", ws, &contract.MemoryHint{Mode: contract.MemoryModeHint, TraceID: 7, SimilarityBp: 9100, Injected: true}))
	complete(t, h, memCall("c2", ws, &contract.MemoryHint{Mode: contract.MemoryModeObserve, TraceID: 8, SimilarityBp: 8300}))
	complete(t, h, memCall("c3", ws, nil))
	// a hint that names no trace records nothing and is not an error (the session ends without a text answer: abandoned)
	complete(t, h, memCall("c4", ws, &contract.MemoryHint{Mode: contract.MemoryModeHint}))
	h.OnSessionClosed(ctx, tollgate.Session{ID: "sess-1", TenantID: ws})
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}

	if n := scalar[int](t, s, `SELECT count(*) FROM memory_hits WHERE workspace_id = $1`, ws); n != 2 {
		t.Fatalf("%d hit rows, want 2 (the hinted call and the observed one)", n)
	}
	if n := scalar[int](t, s, `SELECT count(*) FROM memory_hits WHERE workspace_id = $1 AND injected AND mode = 'hint' AND trace_id = 7 AND similarity_bp = 9100`, ws); n != 1 {
		t.Fatalf("the injected hit is not recorded as sent: %d", n)
	}
	mu.Lock()
	if len(closed) != 1 || closed[0].WorkspaceID != ws || closed[0].SessionID != "sess-1" || closed[0].Outcome != contract.OutcomeAbandoned || closed[0].Calls != 4 {
		t.Fatalf("closed sessions = %+v", closed)
	}
	mu.Unlock()

	// A replay of the same call id (the ledger is idempotent on call_id) adds no second hit.
	h2 := newHook(t, s, nil)
	complete(t, h2, memCall("c1", ws, &contract.MemoryHint{Mode: contract.MemoryModeHint, TraceID: 7, SimilarityBp: 9100, Injected: true}))
	if err := h2.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int](t, s, `SELECT count(*) FROM memory_hits WHERE workspace_id = $1`, ws); n != 2 {
		t.Fatalf("a replayed call wrote %d hit rows, want 2 in all", n)
	}

	now := time.Now().UTC()
	sr, err := contract.ParseSavingsRange("7d", "", "", now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Overview(ctx, ws, sr)
	if err != nil {
		t.Fatal(err)
	}
	port := ledger.NewMemoryPort(s)
	injected := scalar[int64](t, s, `SELECT id FROM memory_hits WHERE workspace_id = $1 AND injected`, ws)
	observed := scalar[int64](t, s, `SELECT id FROM memory_hits WHERE workspace_id = $1 AND NOT injected`, ws)
	for range 2 { // the second credit of the same hit changes nothing
		if err := port.CreditMemory(ctx, ws, "sess-1", injected, 1200, 5000); err != nil {
			t.Fatal(err)
		}
	}
	if err := port.CreditMemory(ctx, ws, "sess-1", observed, 900, 4000); err != nil { // observed hits carry no money
		t.Fatal(err)
	}
	if err := port.CreditMemory(ctx, ws, "sess-1", injected, -1, 0); err == nil {
		t.Error("a negative credit must be refused")
	}
	after, err := s.Overview(ctx, ws, sr)
	if err != nil {
		t.Fatal(err)
	}
	if got := after.Hero.LikelySavedUsd - before.Hero.LikelySavedUsd; got != 5000 {
		t.Errorf("likely rose by %d, want 5000 (one credit, once)", got)
	}
	if after.Hero.SavedUsd != before.Hero.SavedUsd || after.Hero.SavedTokens != before.Hero.SavedTokens {
		t.Errorf("memory moved the certain number: %d/%d -> %d/%d", before.Hero.SavedUsd, before.Hero.SavedTokens, after.Hero.SavedUsd, after.Hero.SavedTokens)
	}
	if n := scalar[int64](t, s, `SELECT (likely_saved_usd * 1000000)::bigint FROM sessions WHERE workspace_id = $1 AND id = 'sess-1'`, ws); n != 5000 {
		t.Errorf("the session's likely number is %d micro-USD, want 5000", n)
	}
	var memoryCard *contract.CauseView
	for i := range after.ByCause {
		if after.ByCause[i].Cause == contract.CauseMemory {
			memoryCard = &after.ByCause[i]
		}
	}
	if memoryCard == nil || memoryCard.Fires != 1 || memoryCard.LikelySavedUsd != 5000 || memoryCard.SavedUsd != 0 {
		t.Errorf("memory cause row = %+v", memoryCard)
	}
}

func TestMemoryHitFeedbackIsValidatedAndScopedToTheWorkspace(t *testing.T) {
	s, ws := pgStore(t)
	h := newHook(t, s, nil)
	ctx := context.Background()
	complete(t, h, memCall("c1", ws, &contract.MemoryHint{Mode: contract.MemoryModeHint, TraceID: 3, SimilarityBp: 9000, Injected: true}))
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	id := scalar[int64](t, s, `SELECT id FROM memory_hits WHERE workspace_id = $1`, ws)
	port := ledger.NewMemoryPort(s)
	up := "helpful"
	ref, err := port.SetHitFeedback(ctx, ws, id, &up)
	if err != nil || ref.TraceID != 3 {
		t.Fatalf("feedback: %+v %v", ref, err)
	}
	bad := "great"
	if _, err := port.SetHitFeedback(ctx, ws, id, &bad); err == nil {
		t.Error(`"great" is not a feedback value`)
	}
	other, err := s.CreateWorkspace(ctx, store.OrgID(), "Other", "BBBBBBBBBBBBBBBBBBBBBB", 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := port.SetHitFeedback(ctx, other.ID, id, &up); err == nil {
		t.Error("another workspace set feedback on a hit that is not its own")
	}
	if _, err := port.SetHitFeedback(ctx, ws, id, nil); err != nil { // clearing is allowed
		t.Fatal(err)
	}
	if n := scalar[int](t, s, `SELECT count(*) FROM memory_hits WHERE workspace_id = $1 AND feedback IS NOT NULL`, ws); n != 0 {
		t.Errorf("feedback not cleared: %d rows", n)
	}
}
