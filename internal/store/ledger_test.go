package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

var base = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

func mu(v int64) *contract.MicroUSD { m := contract.MicroUSD(v); return &m }

func newWS(t *testing.T, s *store.Store, name, pub string) string {
	t.Helper()
	ws, err := s.CreateWorkspace(context.Background(), store.OrgID(), name, pub, 50)
	if err != nil {
		t.Fatal(err)
	}
	return ws.ID
}

func rec(ws, id, sess string, idx int, at time.Time, finish string, text bool, cost *contract.MicroUSD) contract.CallRecord {
	return contract.CallRecord{
		ID: id, OrgID: store.OrgID(), WorkspaceID: ws, SessionID: sess, CallIndex: idx, StartedAt: at, Provider: "openai", Model: "gpt-4o",
		Wire: "openai.chat", Stream: true, Status: 200, Finish: finish, Usage: contract.Usage{Input: 100, Output: 10, Cached: 20, Reasoning: 1},
		Cost: cost, TTFBMs: 40, DurationMs: 1000, ToolCalls: []string{"read", "grep"}, SessionSource: contract.SessionSourceHeader,
		SessionStart: at, LastHadText: text, Client: "opencode",
	}
}

func TestWriteCallsIsIdempotentAndAggregatesSessions(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	batch := []contract.CallRecord{
		rec(ws, "c1", "s1", 1, base, "ok", false, mu(700)),
		rec(ws, "c2", "s1", 2, base.Add(2*time.Second), "ok", true, nil), // unpriced model: NULL cost, flagged
		rec(ws, "c3", "s2", 1, base.Add(time.Second), "ok", true, mu(50)),
	}
	res, err := s.WriteCalls(ctx, batch)
	if err != nil || res.Inserted != 3 || res.Duplicates != 0 {
		t.Fatalf("first write = %+v, %v", res, err)
	}
	again, err := s.WriteCalls(ctx, batch) // a retried batch: no row, no double count
	if err != nil || again.Inserted != 0 || again.Duplicates != 3 {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	sess, calls, err := s.Session(ctx, ws, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Calls != 2 || sess.InputTokens != 200 || sess.OutputTokens != 20 || sess.Cost == nil || *sess.Cost != 700 || sess.Outcome != contract.OutcomeOpen || sess.Source != "header" || sess.Client != "opencode" {
		t.Fatalf("session = %+v", sess)
	}
	if len(calls) != 2 || calls[0].ID != "c1" || calls[1].ID != "c2" || calls[1].Cost != nil || *calls[0].Cost != 700 {
		t.Fatalf("calls = %+v", calls)
	}
	c := calls[0]
	if c.Usage != (contract.Usage{Input: 100, Output: 10, Cached: 20, Reasoning: 1}) || c.TTFBMs != 40 || c.DurationMs != 1000 || len(c.ToolCalls) != 2 || c.ToolCalls[0] != "read" || !c.Stream {
		t.Fatalf("call c1 = %+v", c)
	}
	// a session whose calls were all unpriced keeps a NULL cost, not 0
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{rec(ws, "c4", "s3", 1, base, "ok", true, nil)}); err != nil {
		t.Fatal(err)
	}
	if s3, _, _ := s.Session(ctx, ws, "s3"); s3.Cost != nil {
		t.Fatalf("all-unpriced session cost = %v, want NULL", *s3.Cost)
	}
	// last_call_at moved to the newest call end, via the setup status
	st, err := s.CallStatus(ctx, ws, base.Truncate(24*time.Hour))
	if err != nil || st.CallsToday != 4 || st.FirstCallAt == nil || !st.FirstCallAt.Equal(base) {
		t.Fatalf("status = %+v, %v", st, err)
	}
	w, err := s.Workspace(ctx, store.OrgID(), ws)
	if err != nil || w.LastCallAt == nil || !w.LastCallAt.Equal(base.Add(3*time.Second)) {
		t.Fatalf("last_call_at = %v, %v", w.LastCallAt, err)
	}
}

func TestSessionsAreKeyedByWorkspace(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	a, b := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA"), newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{rec(a, "x1", "same", 1, base, "ok", true, nil), rec(b, "x2", "same", 1, base, "ok", true, nil)}); err != nil {
		t.Fatal(err)
	}
	for _, ws := range []string{a, b} {
		if sess, calls, err := s.Session(ctx, ws, "same"); err != nil || sess.Calls != 1 || len(calls) != 1 || sess.WorkspaceID != ws {
			t.Fatalf("workspace %s: %+v %v %v", ws, sess, calls, err)
		}
	}
	if _, _, err := s.Session(ctx, missingID, "same"); code(err) != contract.CodeNotFound {
		t.Fatalf("a session in another workspace must be not found: %v", err)
	}
}

func TestCloseSessionOutcomeHeuristic(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	tests := []struct {
		sess   string
		finish string
		text   bool
		want   string
	}{
		{"done", "ok", true, contract.OutcomeCompleted},
		{"silent", "ok", false, contract.OutcomeAbandoned},
		{"cancelled", "client_cancel", true, contract.OutcomeAbandoned},
		{"failed", "upstream_error", false, contract.OutcomeAbandoned},
		{"timeout", "timeout", true, contract.OutcomeAbandoned},
	}
	for i, tc := range tests {
		if _, err := s.WriteCalls(ctx, []contract.CallRecord{
			rec(ws, "first-"+tc.sess, tc.sess, 1, base, "upstream_error", false, nil), // an earlier failure does not decide the outcome
			rec(ws, "last-"+tc.sess, tc.sess, 2, base.Add(time.Second*time.Duration(i+1)), tc.finish, tc.text, nil),
		}); err != nil {
			t.Fatal(err)
		}
		closedAt := base.Add(time.Hour)
		if ok, err := s.CloseSession(ctx, ws, tc.sess, closedAt); err != nil || !ok {
			t.Fatalf("%s: close = %v, %v", tc.sess, ok, err)
		}
		got, _, err := s.Session(ctx, ws, tc.sess)
		if err != nil || got.Outcome != tc.want || got.ClosedAt == nil || !got.ClosedAt.Equal(closedAt) {
			t.Errorf("%s: outcome %q closed %v, want %q (%v)", tc.sess, got.Outcome, got.ClosedAt, tc.want, err)
		}
	}
	if ok, err := s.CloseSession(ctx, ws, "done", base.Add(2*time.Hour)); err != nil || ok {
		t.Fatalf("closing a closed session changed it: %v %v", ok, err)
	}
	if ok, err := s.CloseSession(ctx, ws, "nope", base); err != nil || ok {
		t.Fatalf("closing an unknown session: %v %v", ok, err)
	}
	// a later call reopens a closed session
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{rec(ws, "again", "done", 3, base.Add(3*time.Hour), "ok", true, mu(5))}); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.Session(ctx, ws, "done"); got.Outcome != contract.OutcomeOpen || got.ClosedAt != nil || got.Calls != 3 {
		t.Fatalf("reopen: %+v", got)
	}
}

func TestListSessionsPagesByKeysetAndRange(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	other := newWS(t, s, "B", "BBBBBBBBBBBBBBBBBBBBBB")
	var recs []contract.CallRecord
	for i := range 5 {
		recs = append(recs, rec(ws, "p"+string(rune('a'+i)), "sess-"+string(rune('a'+i)), 1, base.Add(time.Duration(i)*time.Hour), "ok", true, mu(10)))
	}
	recs = append(recs, rec(other, "o1", "sess-z", 1, base, "ok", true, mu(10)))
	if _, err := s.WriteCalls(ctx, recs); err != nil {
		t.Fatal(err)
	}
	p1, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Limit: 2})
	if err != nil || len(p1.Items) != 2 || p1.NextCursor == "" || p1.Items[0].ID != "sess-e" || p1.Items[1].ID != "sess-d" {
		t.Fatalf("page 1 = %+v, %v", p1, err)
	}
	p2, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Limit: 2, Cursor: p1.NextCursor})
	if err != nil || len(p2.Items) != 2 || p2.Items[0].ID != "sess-c" || p2.NextCursor == "" {
		t.Fatalf("page 2 = %+v, %v", p2, err)
	}
	p3, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, Limit: 2, Cursor: p2.NextCursor})
	if err != nil || len(p3.Items) != 1 || p3.Items[0].ID != "sess-a" || p3.NextCursor != "" {
		t.Fatalf("page 3 = %+v, %v", p3, err)
	}
	rng, err := s.ListSessions(ctx, contract.SessionQuery{WorkspaceID: ws, From: base.Add(time.Hour), To: base.Add(3 * time.Hour)})
	if err != nil || len(rng.Items) != 2 || rng.Items[0].ID != "sess-c" {
		t.Fatalf("range = %+v, %v", rng, err)
	}
	for name, q := range map[string]contract.SessionQuery{
		"bad cursor": {WorkspaceID: ws, Cursor: "!!"}, "bad workspace": {WorkspaceID: "nope"}, "limit": {WorkspaceID: ws, Limit: 201}, "negative": {WorkspaceID: ws, Limit: -1},
	} {
		if _, err := s.ListSessions(ctx, q); code(err) != contract.CodeValidationFailed {
			t.Errorf("%s: code = %v, want validation_failed (%v)", name, code(err), err)
		}
	}
}

func TestStatsSumsPricedSpendPerUTCDay(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	d1 := time.Date(2026, 3, 9, 23, 30, 0, 0, time.UTC)
	d2 := time.Date(2026, 3, 10, 0, 30, 0, 0, time.UTC)
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{
		rec(ws, "a", "s1", 1, d1, "ok", true, mu(1_500_000)), rec(ws, "b", "s1", 2, d2, "ok", true, mu(250_000)),
		rec(ws, "c", "s2", 1, d2, "ok", true, nil),
	}); err != nil {
		t.Fatal(err)
	}
	st, err := s.Stats(ctx, ws, time.Date(2026, 3, 9, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC))
	if err != nil || st.Spend != 1_750_000 || st.Calls != 3 || st.Sessions != 2 || len(st.ByDay) != 2 {
		t.Fatalf("stats = %+v, %v", st, err)
	}
	if st.ByDay[0].Day.Day() != 9 || st.ByDay[0].Spend != 1_500_000 || st.ByDay[0].Calls != 1 || st.ByDay[1].Spend != 250_000 || st.ByDay[1].Calls != 2 || st.ByDay[1].Sessions != 2 {
		t.Fatalf("by day = %+v", st.ByDay)
	}
	empty, err := s.Stats(ctx, missingID, time.Time{}, base)
	if err != nil || empty.Calls != 0 || empty.Spend != 0 {
		t.Fatalf("empty = %+v, %v", empty, err)
	}
}

func TestPricingIsSeeded(t *testing.T) {
	s := openMigrated(t)
	rows, err := s.Pricing(context.Background())
	if err != nil || len(rows) < 10 {
		t.Fatalf("pricing = %d rows, %v", len(rows), err)
	}
	seen := map[string]bool{}
	for _, r := range rows {
		seen[r.Provider] = true
		if r.InputPerM <= 0 || r.OutputPerM <= 0 || r.EffectiveFrom.IsZero() {
			t.Errorf("bad row %+v", r)
		}
	}
	if !seen["openai"] || !seen["anthropic"] {
		t.Fatalf("providers seeded = %v", seen)
	}
}
