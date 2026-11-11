package store_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/store"
)

// Ruling S7: no materialised view until a measurement says so. The shape is the plan's: 50k calls over 30 days,
// ~5k sessions and ~5k monitor events, all one workspace. A p95 at or over the budget is the signal to stop and
// ask for a migration, not to loosen this test.
const (
	benchCalls    = 50_000
	benchRuns     = 100
	benchWarmups  = 3
	benchP95      = 300 * time.Millisecond
	benchSessions = benchCalls / 10
)

// seedBench fills one workspace with generate_series rows: call g is call g%10 of session g/10, 51 s apart going
// back from now (30 days in all); every 10th call carries one monitor event, rotating over the six monitors,
// the three actions and both confidences.
func seedBench(t *testing.T, s *store.Store, ws string) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO calls (id, org_id, workspace_id, session_id, call_index, started_at, provider, model, wire, stream, status, finish,
		                    input_tokens, output_tokens, cost_usd, tool_calls, blocked, client,
		                    trim_action, trim_confidence, trim_tokens, saved_usd_trim, router_action, routed_from, routed_to, router_score_bp, saved_usd_router)
		 SELECT 'c' || g, $1::uuid, $2::uuid, 's' || (g / 10), g % 10, now() - (g * interval '51 seconds'), 'openai', 'gpt-4o', 'openai.chat', true, 200, 'ok',
		        1000 + g % 500, 100 + g % 50, ((g % 90) + 1) / 1000.0, '["read","grep"]'::jsonb, g % 40 = 0,
		        (ARRAY['opencode','claude-code','codex','curl',NULL])[1 + g % 5],
		        CASE WHEN g % 7 = 0 THEN (ARRAY['acted','observe'])[1 + (g / 7) % 2] END, CASE WHEN g % 7 = 0 THEN (ARRAY['certain','likely'])[1 + (g / 7) % 2] END,
		        CASE WHEN g % 7 = 0 THEN 200 + g % 300 ELSE 0 END, CASE WHEN g % 7 = 0 THEN ((g % 30) + 1) / 1000.0 ELSE 0 END,
		        CASE WHEN g % 11 = 0 THEN (ARRAY['routed','observe'])[1 + (g / 11) % 2] END, CASE WHEN g % 11 = 0 THEN 'gpt-4o' END,
		        CASE WHEN g % 11 = 0 THEN 'gpt-4o-mini' END, CASE WHEN g % 11 = 0 THEN 1500 END, CASE WHEN g % 11 = 0 THEN ((g % 20) + 1) / 1000.0 ELSE 0 END
		 FROM generate_series(0, $3::int - 1) AS g`,
		`INSERT INTO sessions (id, org_id, workspace_id, source, started_at, last_seen_at, calls, input_tokens, output_tokens, cost_usd, client)
		 SELECT 's' || (g / 10), $1::uuid, $2::uuid, 'header', now() - ((g / 10) * 10 * interval '51 seconds') - interval '9 minutes',
		        now() - ((g / 10) * 10 * interval '51 seconds'), 10, 10000, 1000, 0.5, (ARRAY['opencode','claude-code','codex','curl',NULL])[1 + (g / 10) % 5]
		 FROM generate_series(0, $3::int - 1, 10) AS g`,
		`INSERT INTO monitor_events (org_id, workspace_id, session_id, call_id, call_index, ts, monitor, mode, action, evidence,
		                             est_saved_tokens, est_saved_usd, confidence)
		 SELECT $1::uuid, $2::uuid, 's' || (g / 10), 'c' || g, g % 10, now() - (g * interval '51 seconds'),
		        (ARRAY['duplicate_tool_call','repeated_read','no_progress','step_budget','reasoning_loop','second_guessing'])[1 + (g / 10) % 6],
		        'block', (ARRAY['block','note','observe'])[1 + (g / 10) % 3],
		        jsonb_build_object('tool', 'read', 'path', 'src/f' || (g % 50) || '.go', 'pathHash', md5(g::text), 'count', 3),
		        100 + g % 900, ((g % 97) + 1) / 100.0, (ARRAY['certain','likely'])[1 + (g / 10) % 2]
		 FROM generate_series(0, $3::int - 1, 10) AS g`,
	} {
		if _, err := s.Pool().Exec(ctx, q, store.OrgID(), ws, benchCalls); err != nil {
			t.Fatal(err)
		}
	}
	// VACUUM ANALYZE (one statement each: it cannot run inside a transaction) gives the planner statistics and
	// sets hint bits and the visibility map, so no timed read pays for the fresh rows.
	for _, table := range []string{"calls", "sessions", "monitor_events"} {
		if _, err := s.Pool().Exec(ctx, `VACUUM (ANALYZE) `+table); err != nil {
			t.Fatal(err)
		}
	}
}

func p95Of(t *testing.T, name string, run func() error) time.Duration {
	t.Helper()
	for range benchWarmups { // untimed warm-ups, like a hot dashboard: plan, caches and connection are settled
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	ds := make([]time.Duration, benchRuns)
	for i := range ds {
		start := time.Now()
		if err := run(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ds[i] = time.Since(start)
	}
	slices.Sort(ds)
	p95 := ds[(benchRuns*95+99)/100-1]
	t.Logf("%-14s p50 %6.1f ms  p95 %6.1f ms  max %6.1f ms", name, ms(ds[benchRuns/2]), ms(p95), ms(ds[benchRuns-1]))
	return p95
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// TestSavingsReadsStayUnderBudgetAt50kCalls is the S7 bench: 100 runs of each read (the p95 tolerates five slow outliers) over a 30-day range.
func TestSavingsReadsStayUnderBudgetAt50kCalls(t *testing.T) {
	nonet.SkipTiming(t)
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "Bench", "BBBBBBBBBBBBBBBBBBBBBB")
	seedBench(t, s, ws)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	r := contract.SavingsRange{Range: "30d", From: today.AddDate(0, 0, -29), To: today}
	var nCalls, nSess, nEv int
	for q, dst := range map[string]*int{`SELECT count(*) FROM calls`: &nCalls, `SELECT count(*) FROM sessions`: &nSess, `SELECT count(*) FROM monitor_events`: &nEv} {
		if err := s.Pool().QueryRow(ctx, q).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("seeded %d calls, %d sessions, %d events", nCalls, nSess, nEv)
	if nCalls != benchCalls || nSess != benchSessions || nEv != benchCalls/10 {
		t.Fatalf("seed = %d/%d/%d", nCalls, nSess, nEv)
	}
	reads := []struct {
		name string
		run  func() error
	}{
		{"overview", func() error { _, err := s.Overview(ctx, ws, r); return err }},
		{"causes", func() error { _, _, err := s.Causes(ctx, ws, r); return err }},
		{"saves p1", func() error { _, err := s.Saves(ctx, ws, r, "", 20); return err }},
		{"session detail", func() error { _, _, _, _, err := s.SessionDetail(ctx, ws, "s100"); return err }},
	}
	for _, rd := range reads {
		if p95 := p95Of(t, rd.name, rd.run); p95 >= benchP95 {
			t.Errorf("%s p95 = %v, budget %v: stop and ask for a materialised view (ADR 0025), do not loosen this test", rd.name, p95, benchP95)
		}
	}
}
