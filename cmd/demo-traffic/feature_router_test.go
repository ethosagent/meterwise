package main

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// routerStats runs the demo with the given router flag and reads the router's own stats for the workspace.
func routerStats(t *testing.T, mode string) (contract.RouterStats, *store.Store, string) {
	t.Helper()
	ctx := context.Background()
	dsn, st, ws := demoDB(t)
	var out bytes.Buffer
	if _, err := run(ctx, config{dsn: dsn, rounds: 1, router: mode}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	now := time.Now().UTC()
	sr, err := contract.ParseSavingsRange("7d", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	stats, err := store.NewRouterReader(st, nil).Stats(ctx, ws, sr)
	if err != nil {
		t.Fatal(err)
	}
	return stats, st, ws
}

func TestConservativeDemoRoutesReadStepsHoldsTheRestAndCountsTheRefusedTarget(t *testing.T) {
	stats, st, ws := routerStats(t, "conservative")
	if stats.RoutedCalls == 0 || stats.SavedUsd <= 0 {
		t.Fatalf("conservative routed %d calls and saved %v", stats.RoutedCalls, stats.SavedUsd)
	}
	if stats.ObservedCalls != 0 {
		t.Errorf("conservative recorded %d observe calls", stats.ObservedCalls)
	}
	if stats.Unavailable != 1 {
		t.Errorf("the refused gpt-4o-mini target was counted %d times, want 1", stats.Unavailable)
	}
	pair := func(from string) bool {
		for _, p := range stats.ByPair {
			if p.From == from {
				return p.Calls > 0
			}
		}
		return false
	}
	if !pair("gpt-4.1") {
		t.Errorf("no routed gpt-4.1 calls (the dated id folds into the base pair): %+v", stats.ByPair)
	}
	if n := scalar(t, st, `SELECT count(*) FROM calls WHERE workspace_id = $1 AND routed_from = 'claude-opus-4-5'`, ws); n != 0 {
		t.Errorf("%d calls routed to a target that takes no tools", n)
	}
	// call_index is 1-based: the first two calls of every session stay on the strong model.
	if n := scalar(t, st, `SELECT count(*) FROM calls WHERE workspace_id = $1 AND router_action = 'routed' AND call_index <= 2`, ws); n != 0 {
		t.Errorf("%d routed calls among the first two of a session", n)
	}
}

func TestObserveDemoRecordsWhatItWouldSaveAndRoutesNothing(t *testing.T) {
	stats, _, _ := routerStats(t, "observe")
	if stats.RoutedCalls != 0 || stats.ObservedCalls == 0 || stats.WouldSaveUsd <= 0 || stats.SavedUsd != 0 {
		t.Fatalf("observe: routed %d, observed %d, would save %v, saved %v", stats.RoutedCalls, stats.ObservedCalls, stats.WouldSaveUsd, stats.SavedUsd)
	}
}

func TestTheDefaultFlagsPlayNoRouterScenario(t *testing.T) {
	stats, _, _ := routerStats(t, "")
	if stats.RoutedCalls != 0 || stats.ObservedCalls != 0 {
		t.Fatalf("router off yet %d routed, %d observed", stats.RoutedCalls, stats.ObservedCalls)
	}
}
