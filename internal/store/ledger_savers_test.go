package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func trimRec(r contract.CallRecord, action contract.TrimAction, conf contract.Confidence, tokens int64, usd contract.MicroUSD, stages ...contract.TrimStage) contract.CallRecord {
	r.TrimAction, r.TrimConfidence, r.TrimTokens, r.SavedUSDTrim = &action, &conf, tokens, usd
	per := tokens / int64(max(len(stages), 1))
	for _, st := range stages {
		r.TrimEvents = append(r.TrimEvents, contract.TrimEventRecord{
			OrgID: r.OrgID, WorkspaceID: r.WorkspaceID, SessionID: r.SessionID, CallID: r.ID, TS: r.StartedAt, Stage: st, Tool: "bash",
			Regions: 1, BytesBefore: 900, BytesAfter: 300, EstTokens: per, Action: action,
		})
	}
	return r
}

func routeRec(r contract.CallRecord, action contract.RouteAction, usd contract.MicroUSD) contract.CallRecord {
	score := 1200
	r.RouterAction, r.RoutedFrom, r.RoutedTo, r.RouterScoreBp, r.SavedUSDRouter = &action, "gpt-4o", "gpt-4o-mini", &score, usd
	return r
}

type sessionSavers struct {
	saved, likely, trimTokens int64
	routed, hints             int
}

func TestWriteCallsRecordsSaversAndSumsThemIntoTheSession(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")

	at := func(i int) time.Time { return base.Add(time.Duration(i) * time.Second) }
	hint := rec(ws, "c6", "s1", 6, at(6), "ok", true, mu(10))
	hint.HintInjected = true
	batch := []contract.CallRecord{
		trimRec(rec(ws, "c1", "s1", 1, at(1), "ok", false, mu(10)), contract.TrimActed, contract.ConfidenceCertain, 1000, 2500, contract.StageRepeatCollapse, contract.StageDupLines),
		trimRec(rec(ws, "c2", "s1", 2, at(2), "ok", false, mu(10)), contract.TrimActed, contract.ConfidenceLikely, 500, 1500, contract.StageLogWindow),
		trimRec(rec(ws, "c3", "s1", 3, at(3), "ok", false, mu(10)), contract.TrimObserve, contract.ConfidenceLikely, 300, 700, contract.StageJSONTable),
		routeRec(rec(ws, "c4", "s1", 4, at(4), "ok", false, mu(10)), contract.RouteRouted, 3000),
		routeRec(rec(ws, "c5", "s1", 5, at(5), "ok", false, mu(10)), contract.RouteObserve, 900),
		hint,
		rec(ws, "c7", "s1", 7, at(7), "ok", true, mu(10)), // untouched by any saver: NULLs and zeros
	}
	if res, err := s.WriteCalls(ctx, batch); err != nil || res.Inserted != 7 {
		t.Fatalf("write = %+v, %v", res, err)
	}

	type row struct {
		trimAction, trimConf, routerAction, from, to *string
		trimTokens                                   int64
		savedTrim, savedRouter                       string
		score                                        *int32
		hint                                         bool
	}
	read := func(id string) row {
		var r row
		err := conn.QueryRow(ctx, `SELECT trim_action, trim_confidence, trim_tokens, saved_usd_trim::text, router_action, routed_from, routed_to,
			router_score_bp, saved_usd_router::text, hint_injected FROM calls WHERE workspace_id = $1 AND id = $2`, ws, id).
			Scan(&r.trimAction, &r.trimConf, &r.trimTokens, &r.savedTrim, &r.routerAction, &r.from, &r.to, &r.score, &r.savedRouter, &r.hint)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	str := func(p *string) string {
		if p == nil {
			return "<null>"
		}
		return *p
	}
	if r := read("c1"); str(r.trimAction) != "acted" || str(r.trimConf) != "certain" || r.trimTokens != 1000 || r.savedTrim != "0.002500" {
		t.Errorf("c1 = %+v", r)
	}
	if r := read("c2"); str(r.trimConf) != "likely" || r.savedTrim != "0.001500" {
		t.Errorf("c2 = %+v", r)
	}
	if r := read("c3"); str(r.trimAction) != "observe" || r.trimTokens != 300 || r.savedTrim != "0.000700" {
		t.Errorf("c3 = %+v", r)
	}
	if r := read("c4"); str(r.routerAction) != "routed" || str(r.from) != "gpt-4o" || str(r.to) != "gpt-4o-mini" || r.score == nil || *r.score != 1200 || r.savedRouter != "0.003000" {
		t.Errorf("c4 = %+v", r)
	}
	if r := read("c5"); str(r.routerAction) != "observe" || r.savedRouter != "0.000900" {
		t.Errorf("c5 = %+v", r)
	}
	if r := read("c6"); !r.hint {
		t.Errorf("c6 = %+v", r)
	}
	if r := read("c7"); r.trimAction != nil || r.routerAction != nil || r.from != nil || r.score != nil || r.hint || r.trimTokens != 0 || r.savedTrim != "0.000000" || r.savedRouter != "0.000000" {
		t.Errorf("an untouched call must have NULLs and zeros: %+v", r)
	}

	// one trim_events row per non-reverted stage; none for a call no saver touched
	events := func() int {
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM trim_events WHERE workspace_id = $1`, ws).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := events(); n != 4 {
		t.Fatalf("trim_events = %d, want 4 (c1 has two stages)", n)
	}
	var ev struct {
		stage, action, tool string
		tokens              int64
		bytesBefore         int
	}
	if err := conn.QueryRow(ctx, `SELECT stage, action, tool_name, est_tokens, bytes_before FROM trim_events WHERE workspace_id = $1 AND call_id = 'c3'`, ws).
		Scan(&ev.stage, &ev.action, &ev.tool, &ev.tokens, &ev.bytesBefore); err != nil || ev.stage != "json_table" || ev.action != "observe" || ev.tool != "bash" || ev.tokens != 300 || ev.bytesBefore != 900 {
		t.Errorf("c3 event = %+v, %v", ev, err)
	}

	sessions := func() sessionSavers {
		var ss sessionSavers
		err := conn.QueryRow(ctx, `SELECT (saved_usd*1000000)::bigint, (likely_saved_usd*1000000)::bigint, trim_tokens, routed_calls, hints
			FROM sessions WHERE workspace_id = $1 AND id = 's1'`, ws).Scan(&ss.saved, &ss.likely, &ss.trimTokens, &ss.routed, &ss.hints)
		if err != nil {
			t.Fatal(err)
		}
		return ss
	}
	// saved = acted certain trim (2500) + routed (3000); likely = acted likely trim (1500); observe is in neither;
	// trim tokens are acted only (1500); routed calls 1; hints 1
	want := sessionSavers{saved: 5500, likely: 1500, trimTokens: 1500, routed: 1, hints: 1}
	if got := sessions(); got != want {
		t.Fatalf("session = %+v, want %+v", got, want)
	}

	// a replayed call_id adds nothing: no row, no event, no session change
	if res, err := s.WriteCalls(ctx, batch); err != nil || res.Inserted != 0 || res.Duplicates != 7 {
		t.Fatalf("replay = %+v, %v", res, err)
	}
	if n := events(); n != 4 {
		t.Errorf("replay changed trim_events: %d", n)
	}
	if got := sessions(); got != want {
		t.Errorf("replay changed the session: %+v", got)
	}
	// a later batch adds on top
	more := routeRec(rec(ws, "c8", "s1", 8, at(8), "ok", true, mu(10)), contract.RouteRouted, 400)
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{more}); err != nil {
		t.Fatal(err)
	}
	if got := sessions(); got.saved != 5900 || got.routed != 2 {
		t.Errorf("session after a second batch = %+v", got)
	}
}

// A real score of 0 bp is a score, not a missing one: it is stored as 0 and never as NULL.
func TestRouterScoreOfZeroIsStoredAsZero(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	r := routeRec(rec(ws, "z1", "sz", 1, base, "ok", false, mu(10)), contract.RouteRouted, 100)
	zero := 0
	r.RouterScoreBp = &zero
	if _, err := s.WriteCalls(ctx, []contract.CallRecord{r}); err != nil {
		t.Fatal(err)
	}
	var score *int32
	if err := conn.QueryRow(ctx, `SELECT router_score_bp FROM calls WHERE workspace_id = $1 AND id = 'z1'`, ws).Scan(&score); err != nil {
		t.Fatal(err)
	}
	if score == nil || *score != 0 {
		t.Fatalf("score = %v, want 0", score)
	}
}
