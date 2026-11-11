package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// The 0018-0022 seeds: every default route has both ends priced, every target has a verified capability row, and
// the capability seed never claims what a provider page did not state.
func TestStepZeroSeedsAreConsistent(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	type pair struct{ provider, from, to string }
	rows, err := conn.Query(ctx, `SELECT provider, from_model, to_model FROM router_model_routes ORDER BY provider, from_model`)
	if err != nil {
		t.Fatal(err)
	}
	var routes []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.provider, &p.from, &p.to); err != nil {
			t.Fatal(err)
		}
		routes = append(routes, p)
	}
	rows.Close()
	if len(routes) != 6 {
		t.Fatalf("router_model_routes has %d rows, want the 6 default pairs", len(routes))
	}
	priced := func(provider, model string) bool {
		var n int
		err := conn.QueryRow(ctx, `SELECT count(*) FROM pricing WHERE provider = $1 AND model = $2`, provider, model).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
	for _, r := range routes {
		if !priced(r.provider, r.from) || !priced(r.provider, r.to) {
			t.Errorf("route %s %s -> %s: both ends must have a pricing row", r.provider, r.from, r.to)
		}
		var maxTools int
		if err := conn.QueryRow(ctx, `SELECT max_tools FROM router_model_capabilities WHERE provider = $1 AND model = $2`, r.provider, r.to).Scan(&maxTools); err != nil {
			t.Errorf("route target %s/%s has no capability row: %v", r.provider, r.to, err)
		}
	}
	// the unverified capability is denied: claude-haiku-4-5's page states no tool use or JSON schema
	var maxTools *int
	var schema bool
	if err := conn.QueryRow(ctx, `SELECT max_tools, supports_json_schema FROM router_model_capabilities WHERE model = 'claude-haiku-4-5'`).Scan(&maxTools, &schema); err != nil {
		t.Fatal(err)
	}
	if maxTools == nil || *maxTools != 0 || schema {
		t.Errorf("claude-haiku-4-5 = max_tools %v json_schema %v; unverified capabilities must be denied", maxTools, schema)
	}
	// the four new prices, exactly the rows of the interface table
	want := map[string][3]int64{
		"openai/gpt-5": {1250000, 10000000, 125000}, "openai/gpt-5-mini": {250000, 2000000, 25000},
		"anthropic/claude-opus-5-5": {4000000, 20000000, 200000}, "anthropic/claude-sonnet-5-5": {2000000, 10000000, 100000},
	}
	for k, w := range want {
		var in, out, cached int64
		var prov, model string
		for i := range k {
			if k[i] == '/' {
				prov, model = k[:i], k[i+1:]
			}
		}
		err := conn.QueryRow(ctx, `SELECT input_per_m, output_per_m, cached_per_m FROM pricing WHERE provider=$1 AND model=$2 AND effective_from = '2026-11-10T00:00:00Z'`, prov, model).Scan(&in, &out, &cached)
		if err != nil || [3]int64{in, out, cached} != w {
			t.Errorf("%s = %d/%d/%d, %v; want %v", k, in, out, cached, err, w)
		}
	}
	// trim_events is a ledger table, so only internal/ledger may write it (H4)
	found := false
	for _, tbl := range contract.LedgerTables {
		found = found || tbl == "trim_events"
	}
	if !found {
		t.Error("contract.LedgerTables lacks trim_events")
	}
}
