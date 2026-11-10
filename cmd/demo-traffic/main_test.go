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

func TestDemoTrafficFillsTheDefaultWorkspaceAndStatsAreSensible(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	svc := tenancy.New(st, nil, tenancy.Config{MaxWorkspaces: 3})
	if err := svc.EnsureDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run(ctx, config{dsn: dsn, rounds: 3}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "replayed 21 calls in 3 sessions") || !strings.Contains(out.String(), "dropped 0") {
		t.Fatalf("output = %q", out.String())
	}
	list, _ := st.ListWorkspaces(ctx, store.OrgID())
	var ws string
	for _, w := range list {
		if w.IsDefault {
			ws = w.ID
		}
	}
	page, err := st.ListSessions(ctx, contractQuery(ws))
	if err != nil || len(page.Items) != 3 {
		t.Fatalf("sessions = %d, want 3 (%v)", len(page.Items), err)
	}
	for _, s := range page.Items {
		if s.Calls != 7 || s.Cost == nil || *s.Cost <= 0 || s.ClosedAt == nil || s.Outcome != "completed" {
			t.Fatalf("session %s: calls %d cost %v closed %v outcome %s", s.ID, s.Calls, s.Cost, s.ClosedAt, s.Outcome)
		}
	}
	if err := run(ctx, config{dsn: dsn, rounds: 0}, &out); err == nil {
		t.Fatal("rounds=0 accepted")
	}
	if err := run(ctx, config{rounds: 1}, &out); err == nil {
		t.Fatal("missing DATABASE_URL accepted")
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkConfig(tc.cfg)
			if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("checkConfig = %v, want error containing %q", err, tc.want)
			}
		})
	}
}
