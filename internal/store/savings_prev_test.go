package store_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/store"

	"github.com/ethosagent/meterwise/internal/contract"
)

// The previous-window average is read for the returned clients only: a busy previous window with more distinct
// clients than the old 1000-row cap must not null a current client's trend (GLM 07be #5).
func TestCausesPrevAverageSurvivesAManyClientPreviousWindow(t *testing.T) {
	s, ctx := openMigrated(t), context.Background()
	ws := newWS(t, s, "A", "AAAAAAAAAAAAAAAAAAAAAA")
	seedSavings(t, s, ws) // opencode: 3 calls in 03-08..03-10, 1 call (s9) on 03-05
	// 1100 other clients, 5 calls each on 03-06: all of them outrank opencode (1 call) in the previous window.
	_, err := s.Pool().Exec(ctx, `INSERT INTO calls (id, org_id, workspace_id, session_id, call_index, started_at, provider, model, wire, stream, status, finish,
	                                  input_tokens, output_tokens, cost_usd, tool_calls, blocked, client)
	       SELECT 'p' || g || '-' || k, $1::uuid, $2::uuid, 'ps' || g, k, $3::timestamptz + (g * interval '1 second'), 'openai', 'gpt-4o', 'openai.chat', true, 200, 'ok',
	              10, 5, 0.001, '[]'::jsonb, false, 'bulk' || g
	       FROM generate_series(0, 1099) AS g, generate_series(0, 4) AS k`, store.OrgID(), ws, day(6))
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := s.Causes(ctx, ws, rangeOf(8, 10))
	if err != nil {
		t.Fatal(err)
	}
	var op *contract.ClientRow
	for i := range got.Clients {
		if got.Clients[i].Client == "opencode" {
			op = &got.Clients[i]
		}
	}
	if op == nil {
		t.Fatalf("opencode missing from %+v", got.Clients)
	}
	if op.PrevAvgCallsPerSession == nil || *op.PrevAvgCallsPerSession != 1 {
		t.Fatalf("prev average = %v, want 1 (the cap on the previous window must not null it)", op.PrevAvgCallsPerSession)
	}
}
