package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

// demoDB migrates a fresh database, makes the default workspace and returns the store and its id.
func demoDB(t *testing.T) (dsn string, st *store.Store, ws string) {
	t.Helper()
	ctx := context.Background()
	dsn = pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if err := tenancy.New(st, nil, tenancy.Config{MaxWorkspaces: 3}).EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	list, _ := st.ListWorkspaces(ctx, store.OrgID())
	for _, w := range list {
		if w.IsDefault {
			ws = w.ID
		}
	}
	return dsn, st, ws
}

// scalar reads one number from the database.
func scalar(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestDemoTrafficFillsTheDefaultWorkspaceAndStatsAreSensible(t *testing.T) {
	ctx := context.Background()
	dsn, st, ws := demoDB(t)
	var out bytes.Buffer
	res, err := run(ctx, config{dsn: dsn, rounds: 3}, &out)
	if err != nil {
		t.Fatal(err)
	}
	// 3 rounds: one plain session and three looping agents each
	if res.clientCalls == 0 || len(res.sessions) != 12 {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{"monitors set to note on workspace " + ws, "replayed ", "in 12 sessions", "dropped 0", "summary:", "monitor events by action and confidence", "saved (certain): $", "likely saved: $", "never summed"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output lacks %q:\n%s", want, out.String())
		}
	}
	page, err := st.ListSessions(ctx, contractQuery(ws))
	if err != nil || len(page.Items) != 12 {
		t.Fatalf("sessions = %d, want 12 (%v)", len(page.Items), err)
	}
	plain := 0
	for _, s := range page.Items {
		if s.ClosedAt == nil || s.Cost == nil || *s.Cost <= 0 {
			t.Fatalf("session %s: closed %v cost %v", s.ID, s.ClosedAt, s.Cost)
		}
		if strings.Count(s.ID, "-") == 2 { // demo-<stamp>-<round>; a looping agent's id ends in its kind
			plain++
			if s.Calls != 7 || s.Outcome != "completed" {
				t.Fatalf("plain session %s: calls %d outcome %s", s.ID, s.Calls, s.Outcome)
			}
		}
	}
	if plain != 3 {
		t.Fatalf("plain sessions = %d, want 3", plain)
	}
	// the monitors ran: events exist for every loop monitor, and note mode moved no certain number
	for _, m := range []string{"duplicate_tool_call", "repeated_read", "no_progress"} {
		if n := scalar(t, st, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1 AND monitor = $2 AND action = 'note'`, ws, m); n == 0 {
			t.Fatalf("no %s note events", m)
		}
	}
	if n := scalar(t, st, `SELECT count(*) FROM calls WHERE workspace_id = $1 AND blocked`, ws); n != 0 {
		t.Fatalf("note mode blocked %d calls", n)
	}
	if _, err := run(ctx, config{dsn: dsn, rounds: 0}, &out); err == nil {
		t.Fatal("rounds=0 accepted")
	}
	if _, err := run(ctx, config{rounds: 1}, &out); err == nil {
		t.Fatal("missing DATABASE_URL accepted")
	}
	if _, err := run(ctx, config{dsn: dsn, rounds: 1, monitors: "loud"}, &out); err == nil {
		t.Fatal("-monitors loud accepted")
	}
}

func TestBlockModeSavesMoneyAndTheUpstreamSeesFewerCalls(t *testing.T) {
	ctx := context.Background()
	dsn, st, ws := demoDB(t)
	var out bytes.Buffer
	res, err := run(ctx, config{dsn: dsn, rounds: 4, monitors: "block"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if res.upstreamHits >= res.clientCalls {
		t.Fatalf("the upstream saw %d of %d client calls: a block must keep calls off it", res.upstreamHits, res.clientCalls)
	}
	if n := scalar(t, st, `SELECT count(*) FROM calls WHERE workspace_id = $1 AND blocked`, ws); n != res.clientCalls-res.upstreamHits {
		t.Fatalf("blocked calls in the ledger = %d, client calls %d - upstream hits %d", n, res.clientCalls, res.upstreamHits)
	}
	if n := scalar(t, st, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1 AND action = 'block' AND confidence = 'certain'`, ws); n == 0 {
		t.Fatal("no certain block event")
	}
	if n := scalar(t, st, `SELECT count(*) FROM sessions WHERE workspace_id = $1 AND saved_usd > 0`, ws); n == 0 {
		t.Fatal("no session saved anything in block mode")
	}
	if !strings.Contains(out.String(), "monitors set to block") || strings.Contains(out.String(), "saved (certain): $0.000000") {
		t.Fatalf("output:\n%s", out.String())
	}
	stored, err := tenancy.New(st, nil, tenancy.Config{MaxWorkspaces: 3}).MonitorConfig(ctx, contract.Principal{OrgID: store.OrgID()}, ws)
	if err != nil || stored.DuplicateToolCall.Mode != contract.ModeBlock || stored.NoProgress.Mode != contract.ModeBlock {
		t.Fatalf("stored config %+v (%v)", stored, err)
	}
}

func TestObserveModeRecordsEventsAndSavesNothing(t *testing.T) {
	ctx := context.Background()
	dsn, st, ws := demoDB(t)
	var out bytes.Buffer
	res, err := run(ctx, config{dsn: dsn, rounds: 2, monitors: "observe"}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if res.upstreamHits != res.clientCalls {
		t.Fatalf("observe must not change what reaches the upstream: %d of %d", res.upstreamHits, res.clientCalls)
	}
	if scalar(t, st, `SELECT count(*) FROM monitor_events WHERE workspace_id = $1`, ws) == 0 ||
		scalar(t, st, `SELECT count(*) FROM sessions WHERE workspace_id = $1 AND (saved_usd <> 0 OR likely_saved_usd <> 0)`, ws) != 0 {
		t.Fatal("observe: want events and no saved number")
	}
}

func contractQuery(ws string) contract.SessionQuery { return contract.SessionQuery{WorkspaceID: ws} }

func TestAnUnknownScenarioFailsFastAndTheShippedScriptIsValid(t *testing.T) {
	if err := validateScript(script); err != nil {
		t.Fatalf("the shipped script: %v", err)
	}
	err := validateScript([]string{"openai-text", "no-such-scenario"})
	if err == nil || !strings.Contains(err.Error(), "no-such-scenario") {
		t.Fatalf("an unknown scenario must be refused by name, got %v", err)
	}
}

func TestCheckConfigRefusesBadInputBeforeAnyWork(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  config
		want string
	}{
		{"ok", config{dsn: "postgres://x", rounds: 1}, ""},
		{"max rounds", config{dsn: "postgres://x", rounds: 1000}, ""},
		{"no dsn", config{rounds: 1}, "DATABASE_URL"},
		{"zero rounds", config{dsn: "postgres://x"}, "-rounds"},
		{"too many rounds", config{dsn: "postgres://x", rounds: 1001}, "-rounds"},
		{"block", config{dsn: "postgres://x", rounds: 1, monitors: "block"}, ""},
		{"bad monitors", config{dsn: "postgres://x", rounds: 1, monitors: "loud"}, "-monitors"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkConfig(tc.cfg)
			if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("checkConfig = %v, want error containing %q", err, tc.want)
			}
		})
	}
}
