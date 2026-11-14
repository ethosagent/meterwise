package store_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// unit returns a vector with 1 at index i: unit(0) and unit(1) are orthogonal (cosine similarity 0), unit(0) with itself is 1.
func unit(i int) []float32 {
	v := make([]float32, store.EmbedDim)
	v[i] = 1
	return v
}

func memDB(t *testing.T) (context.Context, *store.Store, *pgx.Conn, string, string) {
	t.Helper()
	ctx := context.Background()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return ctx, s, conn, newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA"), newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
}

func newTrace(ws, session string, vec []float32, conf float32) store.NewTrace {
	return store.NewTrace{
		WorkspaceID: ws, SessionID: session, TaskSummary: "task " + session, Observations: []string{"o"}, Approach: "approach " + session, Fix: "fix " + session,
		Signature: contract.TraceSignature{Files: []string{"a.go"}}, Confidence: conf, PromptV: 1, EmbedModel: "e", Embedding: vec,
	}
}

func TestTracesAreStoredOncePerSessionAndRecalledPerWorkspace(t *testing.T) {
	ctx, s, _, a, b := memDB(t)
	id, ok, err := s.InsertTrace(ctx, newTrace(a, "s1", unit(0), 0.9))
	if err != nil || !ok || id <= 0 {
		t.Fatalf("insert = %d %v %v", id, ok, err)
	}
	if _, ok, err := s.InsertTrace(ctx, newTrace(a, "s1", unit(0), 0.9)); err != nil || ok {
		t.Fatalf("a replayed close must add nothing: %v %v", ok, err)
	}
	if _, _, err := s.InsertTrace(ctx, newTrace(a, "s2", unit(1), 0.9)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.InsertTrace(ctx, newTrace(a, "s3", unit(2), 0.4)); err != nil { // below the recall confidence floor
		t.Fatal(err)
	}
	if _, _, err := s.InsertTrace(ctx, newTrace(b, "s1", unit(0), 0.9)); err != nil { // another workspace, same vector
		t.Fatal(err)
	}

	got, err := s.NearestTraces(ctx, a, unit(0), 3, 0.6)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].SessionID != "s1" || got[0].Similarity < 0.999 || got[1].SessionID != "s2" || got[1].Similarity > 0.001 {
		t.Fatalf("nearest = %+v: want s1 (1.0) then s2 (0.0), never s3 (confidence 0.4) or the other workspace's trace", got)
	}
	if got[0].Approach != "approach s1" || got[0].Fix != "fix s1" || len(got[0].Signature.Files) != 1 {
		t.Fatalf("candidate = %+v", got[0])
	}
	if one, _ := s.NearestTraces(ctx, a, unit(0), 1, 0.6); len(one) != 1 {
		t.Fatalf("k=1 returned %d", len(one))
	}
	if _, err := s.NearestTraces(ctx, a, []float32{1, 2}, 3, 0.6); err == nil {
		t.Fatal("a vector of the wrong size must be refused")
	}
	if _, _, err := s.InsertTrace(ctx, newTrace(a, "s9", func() []float32 { v := unit(0); v[5] = float32(nan()); return v }(), 0.9)); err == nil {
		t.Fatal("a NaN embedding must be refused")
	}
}

func nan() float64 { z := 0.0; return z / z }

func TestVotesMoveConfidenceByTwoTenths(t *testing.T) {
	ctx, s, _, a, _ := memDB(t)
	id, _, _ := s.InsertTrace(ctx, newTrace(a, "s1", unit(0), 0.7))
	var recs []contract.CallRecord
	var hits []store.MemoryHit
	for i, call := range []string{"c1", "c2", "c3"} {
		recs = append(recs, rec(a, call, "hs", i+1, base.Add(time.Duration(i)*time.Second), "ok", false, mu(10)))
		hits = append(hits, store.MemoryHit{CallID: call, TraceID: id, SimilarityBp: 9000, Mode: contract.MemoryModeHint, Injected: true})
	}
	if res, err := s.WriteCallsWithHits(ctx, recs, hits); err != nil || res.Inserted != 3 {
		t.Fatalf("write = %+v %v", res, err)
	}
	rows, err := s.SessionMemoryHits(ctx, a, "hs")
	if err != nil || len(rows) != 3 {
		t.Fatalf("hits = %+v %v", rows, err)
	}
	h1, h2, h3 := rows[0].ID, rows[1].ID, rows[2].ID
	fb := func(h int64, v string) {
		if _, err := s.SetMemoryHitFeedback(ctx, a, h, &v); err != nil {
			t.Fatal(err)
		}
	}
	conf := func() int {
		bp, err := s.RecountTraceVotes(ctx, a, id)
		if err != nil {
			t.Fatal(err)
		}
		return bp
	}
	fb(h1, "irrelevant")
	fb(h2, "irrelevant")
	if got := conf(); got != 7000 {
		t.Fatalf("two downvotes: confidence %d bp, want 7000", got)
	}
	fb(h3, "irrelevant")
	if got := conf(); got != 5000 {
		t.Fatalf("three downvotes: confidence %d bp, want 5000", got)
	}
	if _, err := s.SetMemoryHitFeedback(ctx, a, h3, nil); err != nil { // clearing a vote takes it back
		t.Fatal(err)
	}
	if got := conf(); got != 7000 {
		t.Fatalf("a cleared vote: confidence %d bp, want 7000", got)
	}
	// A trace below the recall floor (0.6) is no longer recalled once demoted.
	fb(h3, "irrelevant")
	conf()
	if got, _ := s.NearestTraces(ctx, a, unit(0), 3, 0.6); len(got) != 0 {
		t.Fatalf("a demoted trace (0.5) was still recalled: %+v", got)
	}
	// Three upvotes and no downvote promote it by 0.2 (0.7 -> 0.9).
	fb(h1, "helpful")
	fb(h2, "helpful")
	if got := conf(); got != 7000 { // one downvote left, two upvotes: neither rule fires
		t.Fatalf("two upvotes and a downvote: confidence %d bp, want 7000", got)
	}
	fb(h3, "helpful")
	if got := conf(); got != 9000 {
		t.Fatalf("three upvotes: confidence %d bp, want 9000", got)
	}
	// Another workspace cannot vote on this workspace's hit.
	if _, err := s.SetMemoryHitFeedback(ctx, "BBBBBBBBBBBBBBBBBBBBBB", h1, new("helpful")); err == nil {
		t.Fatal("feedback across workspaces must be not_found")
	}
}

func TestForgetPruneAndPaging(t *testing.T) {
	ctx, s, conn, a, b := memDB(t)
	for i := range 5 {
		if _, _, err := s.InsertTrace(ctx, newTrace(a, "s"+strconv.Itoa(i), unit(i), 0.9)); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _ = s.InsertTrace(ctx, newTrace(b, "sb", unit(0), 0.9))
	_ = s.InsertTraceFailure(ctx, store.TraceFailure{WorkspaceID: a, SessionID: "f1", LastError: "boom", Calls: 2})
	_ = s.InsertTraceFailure(ctx, store.TraceFailure{WorkspaceID: a, SessionID: "f1", LastError: "boom", Calls: 2}) // a repeat adds nothing

	page1, err := s.ListTraces(ctx, a, "", 2)
	if err != nil || len(page1.Items) != 2 || page1.NextCursor == nil {
		t.Fatalf("page 1 = %+v %v", page1, err)
	}
	page2, err := s.ListTraces(ctx, a, *page1.NextCursor, 10)
	if err != nil || len(page2.Items) != 3 || page2.NextCursor != nil || page2.Items[0].ID == page1.Items[0].ID {
		t.Fatalf("page 2 = %+v %v", page2, err)
	}
	d, err := s.TraceDetail(ctx, a, page1.Items[0].ID)
	if err != nil || d.Approach == "" || len(d.Observations) != 1 || d.HelpedSessions == nil {
		t.Fatalf("detail = %+v %v", d, err)
	}
	if _, err := s.TraceDetail(ctx, b, page1.Items[0].ID); err == nil {
		t.Fatal("a trace of another workspace must be not_found")
	}

	// Prune: traces unused since the cutoff go, the used and the recent stay.
	if _, err := conn.Exec(ctx, `UPDATE memory_traces SET created_at = now() - interval '200 days' WHERE workspace_id = $1 AND session_id IN ('s0','s1')`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE memory_traces SET last_used_at = now() WHERE workspace_id = $1 AND session_id = 's1'`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE memory_trace_failures SET created_at = now() - interval '200 days'`); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneMemory(ctx, time.Now().AddDate(0, 0, -90))
	if err != nil || n != 1 { // s0 only: s1 was used lately
		t.Fatalf("pruned %d, %v: want only the old unused trace", n, err)
	}
	var f int
	_ = conn.QueryRow(ctx, `SELECT count(*) FROM memory_trace_failures`).Scan(&f)
	if f != 0 {
		t.Fatalf("old failure records must be pruned too, %d left", f)
	}

	if n, err := s.ForgetTraces(ctx, a); err != nil || n != 4 {
		t.Fatalf("forget = %d %v", n, err)
	}
	if got, _ := s.NearestTraces(ctx, a, unit(2), 3, 0); len(got) != 0 {
		t.Fatal("a forgotten trace was still recalled")
	}
	if got, _ := s.NearestTraces(ctx, b, unit(0), 3, 0); len(got) != 1 {
		t.Fatal("forget must not touch another workspace")
	}
}

func TestSelfUsageAndCaptureAllowed(t *testing.T) {
	ctx, s, conn, a, b := memDB(t)
	if err := s.AddSelfUsage(ctx, a, "distil", "m", 100, 50, 1500); err != nil {
		t.Fatal(err)
	}
	_ = s.AddSelfUsage(ctx, a, "embed", "e", 10, 0, 20)
	_ = s.AddSelfUsage(ctx, b, "embed", "e", 10, 0, 999)
	got, err := s.SelfUsageSince(ctx, a, time.Now().Add(-time.Hour))
	if err != nil || got != 1520 {
		t.Fatalf("self usage = %d %v, want 1520 (workspace B's spend is not A's)", got, err)
	}
	if got, _ := s.SelfUsageSince(ctx, a, time.Now().Add(time.Hour)); got != 0 {
		t.Fatalf("future window = %d", got)
	}
	if ok, err := s.WorkspaceCaptureAllowed(ctx, a); err != nil || ok {
		t.Fatalf("capture is off by default: %v %v", ok, err)
	}
	if _, err := conn.Exec(ctx, `UPDATE workspaces SET settings = jsonb_set(settings, '{capture}', 'true') WHERE id = $1`, a); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.WorkspaceCaptureAllowed(ctx, a); !ok {
		t.Fatal("capture on must be allowed")
	}
}
