package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

const (
	wsA = "AAAAAAAAAAAAAAAAAAAAAA"
	wsB = "BBBBBBBBBBBBBBBBBBBBBB"
	wsC = "CCCCCCCCCCCCCCCCCCCCCC"
)

var seededDefaults = []contract.RoutePair{
	{Provider: "openai", From: "gpt-4o", To: "gpt-4o-mini"},
	{Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"},
	{Provider: "openai", From: "gpt-5", To: "gpt-5-mini"},
}

func routed(r contract.CallRecord, from string, usd contract.MicroUSD) contract.CallRecord {
	r = routeRec(r, contract.RouteRouted, usd)
	r.RoutedFrom, r.RoutedTo, r.Model = from, "gpt-4o-mini", "gpt-4o-mini"
	return r
}

func fired(r contract.CallRecord) contract.CallRecord {
	r.Verdicts = []contract.PricedVerdict{verdict(contract.MonitorDuplicateToolCall, contract.ModeObserve, contract.ConfidenceLikely, 10, 100)}
	return r
}

// seedRouter writes, in March 2026 day 9: sessions r1 and r2 (routed calls), sessions p1..p3 (never routed), an
// observed call, and a routed call the upstream answered 404.
//
//	cheap answered calls: 4 (r1: c1 fired, c2, c3 [dated source id]; r2: c4)          fire rate 1/4 = 2500 bp
//	strong answered calls: 10 (p1: 4, p2: 4 with 1 fired, p3: 2) + observed o1 -> 11   fire rate 1/11 = 909 bp
//	routed 404: c5 (unavailable), saved 0
//	blocked: blka (fired), blkb, in session b: counted nowhere
func seedRouter(t *testing.T, s *store.Store, ws string) {
	t.Helper()
	at := func(i int) time.Time { return day(9).Add(time.Duration(i) * time.Minute) }
	var batch []contract.CallRecord
	batch = append(batch,
		fired(routed(rec(ws, "c1", "r1", 3, at(1), "ok", true, mu(100)), "gpt-4o", 3_000_000)),
		routed(rec(ws, "c2", "r1", 4, at(2), "ok", true, mu(100)), "gpt-4o", 2_000_000),
		routed(rec(ws, "c3", "r1", 5, at(3), "ok", true, mu(100)), "gpt-4o-2024-08-06", 1_000_000),
		routed(rec(ws, "c4", "r2", 3, at(4), "ok", true, mu(100)), "gpt-4o", 4_000_000),
	)
	c5 := routed(rec(ws, "c5", "r2", 4, at(5), "upstream_error", false, nil), "gpt-4o", 0)
	c5.Status, c5.Usage = 404, contract.Usage{}
	batch = append(batch, c5)
	o1 := routeRec(rec(ws, "o1", "o", 3, at(6), "ok", true, mu(100)), contract.RouteObserve, 7_000_000)
	batch = append(batch, o1)
	for i := range 10 {
		sess := []string{"p1", "p2", "p3"}[min(i/4, 2)]
		r := rec(ws, "p"+string(rune('a'+i)), sess, i%4+1, at(10+i), "ok", true, mu(100))
		if i == 5 {
			r = fired(r)
		}
		batch = append(batch, r)
	}
	// Blocked calls never got an answer: one that fired a monitor and one that did not. Neither may change a count,
	// the routed share or either fire rate, whichever side of the guardrail it would have landed on.
	for i, f := range []bool{true, false} {
		b := rec(ws, "blk"+string(rune('a'+i)), "b", i+1, at(20+i), "blocked", false, nil)
		b.Blocked, b.Usage = true, contract.Usage{}
		if f {
			b = fired(b)
		}
		batch = append(batch, b)
	}
	if _, err := s.WriteCalls(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	// r1 completes, r2 abandons (its last call was refused); p1, p2, p3 complete; o completes
	var closes []contract.SessionClose
	for _, id := range []string{"r1", "r2", "p1", "p2", "p3", "o"} {
		closes = append(closes, contract.SessionClose{WorkspaceID: ws, SessionID: id, At: at(30)})
	}
	if _, err := s.CloseSessions(context.Background(), closes); err != nil {
		t.Fatal(err)
	}
}

func TestRouterRowsAreTheSeededDefaultsAndCapabilities(t *testing.T) {
	s := openMigrated(t)
	routes, caps, err := s.RouterRows(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 6 || routes[0].Provider != "anthropic" {
		t.Errorf("routes = %+v", routes)
	}
	byModel := map[string]contract.RouterCapRow{}
	for _, c := range caps {
		byModel[c.Provider+"/"+c.Model] = c
	}
	if len(caps) != 5 {
		t.Fatalf("caps = %+v", caps)
	}
	if m := byModel["openai/gpt-5-mini"]; m.MaxTools != nil || !m.JSONSchema || !m.Vision || !m.Reasoning {
		t.Errorf("gpt-5-mini = %+v (NULL max_tools must read back as nil)", m)
	}
	if m := byModel["anthropic/claude-haiku-4-5"]; m.MaxTools == nil || *m.MaxTools != 0 || m.JSONSchema {
		t.Errorf("claude-haiku-4-5 = %+v (max_tools 0 means no tool use)", m)
	}
}

func TestRouterStatsReconcileWithTheLedgerRows(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", wsA), newWS(t, s, "B", wsB)
	seedRouter(t, s, ws)
	seedRouter(t, s, other) // identical rows in another workspace must never be counted
	r := store.NewRouterReader(s, seededDefaults)
	st, err := r.Stats(ctx, ws, rangeOf(8, 10))
	if err != nil {
		t.Fatal(err)
	}
	if st.Calls != 16 || st.RoutedCalls != 5 || st.ObservedCalls != 1 || st.Unavailable != 1 || st.WorseFeedback != 0 {
		t.Errorf("counts = %+v", st)
	}
	if st.SavedUsd != 10_000_000 || st.WouldSaveUsd != 7_000_000 {
		t.Errorf("saved %v would %v: routed 3+2+1+4 certain, observe 7 kept apart", st.SavedUsd, st.WouldSaveUsd)
	}
	if st.RoutedBp == nil || *st.RoutedBp != 3125 { // 5 of 16
		t.Errorf("routedBp = %v", st.RoutedBp)
	}
	g := st.Guardrail
	if g.CheapCalls != 4 || g.StrongCalls != 11 || *g.CheapFireBp != 2500 || *g.StrongFireBp != 909 || !g.Warn {
		t.Errorf("guardrail = %+v", g)
	}
	// r1 completed, r2 abandoned: 5000 bp cheap; p1-p3 and o completed: 10000 bp strong
	if *g.CheapCompletedBp != 5000 || *g.StrongCompletedBp != 10000 {
		t.Errorf("completed = %d / %d", *g.CheapCompletedBp, *g.StrongCompletedBp)
	}
	if len(st.ByPair) != 1 || st.ByPair[0].From != "gpt-4o" || st.ByPair[0].To != "gpt-4o-mini" || st.ByPair[0].Calls != 5 || st.ByPair[0].SavedUsd != 10_000_000 {
		t.Errorf("by pair = %+v: the dated source id counts for its base pair", st.ByPair)
	}
	var sum contract.MicroUSD // reconcile: routed saved == SUM(calls.saved_usd_router) of routed calls == the sessions' share
	for _, id := range []string{"r1", "r2"} {
		sess, _, _, err := s.Session(ctx, ws, id)
		if err != nil {
			t.Fatal(err)
		}
		sum += sess.Saved
	}
	if sum != st.SavedUsd {
		t.Errorf("sessions r1+r2 saved %v != router stats saved %v", sum, st.SavedUsd)
	}
	if empty, err := r.Stats(ctx, ws, rangeOf(1, 2)); err != nil || empty.Calls != 0 || empty.RoutedBp != nil || empty.Guardrail.CheapFireBp != nil || empty.Guardrail.Warn || empty.ByPair == nil {
		t.Errorf("an empty range: %+v %v", empty, err)
	}
}

// The guardrail warns exactly when the cheap fire rate is more than 200 bp above the strong one.
func TestGuardrailWarnsOnlyAboveTwoHundredBasisPoints(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	// 50 calls each: 6/50 = 1200 bp, 7/50 = 1400 bp (diff 200 -> no warning), 8/50 = 1600 bp (diff 400 -> warning)
	for _, tc := range []struct {
		pub         string
		cheapFired  int
		strongFired int
		wantWarn    bool
	}{{wsA, 7, 6, false}, {wsB, 8, 6, true}, {wsC, 0, 5, false}} {
		ws := newWS(t, s, tc.pub, tc.pub)
		var batch []contract.CallRecord
		for i := range 50 {
			c := routed(rec(ws, tc.pub[:1]+"k"+string(rune('a'+i)), "x", 3, day(9).Add(time.Duration(i)*time.Second), "ok", true, mu(1)), "gpt-4o", 1)
			if i < tc.cheapFired {
				c = fired(c)
			}
			batch = append(batch, c)
			p := rec(ws, tc.pub[:1]+"q"+string(rune('a'+i)), "y", 1, day(9).Add(time.Duration(i)*time.Second), "ok", true, mu(1))
			if i < tc.strongFired {
				p = fired(p)
			}
			batch = append(batch, p)
		}
		if _, err := s.WriteCalls(ctx, batch); err != nil {
			t.Fatal(err)
		}
		st, err := store.NewRouterReader(s, seededDefaults).Stats(ctx, ws, rangeOf(9, 9))
		if err != nil {
			t.Fatal(err)
		}
		if st.Guardrail.Warn != tc.wantWarn {
			t.Errorf("cheap %d/50 strong %d/50: warn = %v (%+v)", tc.cheapFired, tc.strongFired, st.Guardrail.Warn, st.Guardrail)
		}
	}
}

func TestSuggestionsAreDefaultPairsForModelsTheWorkspaceUsed(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", wsA), newWS(t, s, "B", wsB)
	now := time.Now().UTC()
	old := now.AddDate(0, 0, -40)
	call := func(w, id, model string, at time.Time) contract.CallRecord {
		r := rec(w, id, "s", 1, at, "ok", true, mu(1))
		r.Model = model
		return r
	}
	batch := []contract.CallRecord{
		call(ws, "a1", "gpt-4.1-2025-04-14", now), call(ws, "a2", "gpt-4.1-2025-04-14", now), call(ws, "a3", "gpt-4o", now),
		call(ws, "a4", "gpt-5", old), // outside 30 days
		call(ws, "a5", "claude-unknown", now),
		call(other, "b1", "gpt-5", now), // another workspace
	}
	if _, err := s.WriteCalls(ctx, batch); err != nil {
		t.Fatal(err)
	}
	// a routed call counts for the model it asked for, not the one it ran on
	rc := routed(rec(ws, "a6", "s", 2, now, "ok", true, mu(1)), "gpt-4o", 1)
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{rc}); err != nil {
		t.Fatal(err)
	}
	got, err := store.NewRouterReader(s, seededDefaults).Suggestions(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	want := []contract.RoutePair{seededDefaults[0], seededDefaults[1]} // gpt-4o seen twice, gpt-4.1 twice: both listed, no gpt-5
	if len(got) != 2 || got[0] != want[0] && got[0] != want[1] || got[0] == got[1] {
		t.Errorf("suggestions = %+v", got)
	}
	for _, p := range got {
		if p.From == "gpt-5" {
			t.Errorf("a model seen only 40 days ago or in another workspace was suggested: %+v", p)
		}
	}
	if _, err := store.NewRouterReader(s, nil).Suggestions(ctx, "not-a-uuid"); code(err) != contract.CodeValidationFailed {
		t.Errorf("bad workspace id: %v", err)
	}
	if d, _ := store.NewRouterReader(s, seededDefaults).Defaults(ctx); len(d) != 3 {
		t.Errorf("defaults = %+v", d)
	}
}

func TestFeedbackSetsReplacesClearsAndIsScopedToTheWorkspace(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws, other := newWS(t, s, "A", wsA), newWS(t, s, "B", wsB)
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{routed(rec(ws, "c1", "s", 3, day(9), "ok", true, mu(1)), "gpt-4o", 5)}); err != nil {
		t.Fatal(err)
	}
	r := store.NewRouterReader(s, seededDefaults)
	worse, fine := "worse", "fine"
	user := "11111111-1111-1111-1111-111111111111"
	v, err := r.SetFeedback(ctx, ws, "c1", user, &worse)
	if err != nil || v.CallID != "c1" || v.Verdict == nil || *v.Verdict != "worse" {
		t.Fatalf("set worse = %+v, %v", v, err)
	}
	if st, _ := r.Stats(ctx, ws, rangeOf(9, 9)); st.WorseFeedback != 1 {
		t.Errorf("worse count = %d", st.WorseFeedback)
	}
	if v, err = r.SetFeedback(ctx, ws, "c1", "not-a-uuid", &fine); err != nil || *v.Verdict != "fine" {
		t.Fatalf("a second verdict replaces the first (and a non-uuid user is stored as no user): %+v, %v", v, err)
	}
	if n := count(t, s, `SELECT count(*) FROM router_feedback WHERE workspace_id = $1::uuid`, ws); n != 1 {
		t.Errorf("feedback rows = %d, want 1 (one per call)", n)
	}
	if st, _ := r.Stats(ctx, ws, rangeOf(9, 9)); st.WorseFeedback != 0 {
		t.Errorf("fine is not worse: %d", st.WorseFeedback)
	}
	if v, err = r.SetFeedback(ctx, ws, "c1", user, nil); err != nil || v.Verdict != nil || v.CallID != "c1" {
		t.Fatalf("clear = %+v, %v", v, err)
	}
	if n := count(t, s, `SELECT count(*) FROM router_feedback`); n != 0 {
		t.Errorf("a cleared verdict leaves no row: %d", n)
	}
	for name, args := range map[string][2]string{"another workspace's call": {other, "c1"}, "an unknown call": {ws, "nope"}, "a malformed workspace": {"x", "c1"}, "an empty call id": {ws, ""}} {
		if _, err := r.SetFeedback(ctx, args[0], args[1], user, &worse); code(err) != contract.CodeNotFound {
			t.Errorf("%s: %v, want not_found", name, err)
		}
	}
	if _, err := r.SetFeedback(ctx, ws, "c1", user, new("meh")); code(err) != contract.CodeValidationFailed {
		t.Errorf("a bad verdict: %v", err)
	}
	if n := count(t, s, `SELECT count(*) FROM router_feedback`); n != 0 {
		t.Errorf("refused feedback wrote %d rows", n)
	}
}
