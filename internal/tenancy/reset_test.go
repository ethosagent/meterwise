package tenancy_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

func TestResetServesTheOldIDInGraceThenReportsItReset(t *testing.T) {
	dir, svc, _, clk := newDir(t)
	ctx := context.Background()
	ws, err := svc.CreateWorkspace(ctx, principal(), "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	old := ws.PublicID
	if _, err := dir.ByPublicID(ctx, old); err != nil { // warm the cache
		t.Fatal(err)
	}
	res, err := dir.Reset(ctx, principal(), ws.ID, 10*time.Minute)
	if err != nil || res.Workspace.PublicID == old || len(res.Workspace.PublicID) != tenancy.PublicIDLength {
		t.Fatalf("reset: %+v %v", res, err)
	}
	if !res.PreviousGraceUntil.Equal(clk.t.Add(10 * time.Minute)) {
		t.Fatalf("grace until %v", res.PreviousGraceUntil)
	}
	got, err := dir.ByPublicID(ctx, old)
	if err != nil || !got.Retired || got.ID != ws.ID || got.GraceUntil == nil {
		t.Fatalf("old id in grace (cache was busted): %+v %v", got, err)
	}
	if cur, err := dir.ByPublicID(ctx, res.Workspace.PublicID); err != nil || cur.Retired {
		t.Fatalf("new id: %+v %v", cur, err)
	}

	// the cached retired entry never outlives its grace, even though the TTL is 30 s
	clk.t = clk.t.Add(10*time.Minute - 5*time.Second)
	if _, err := dir.ByPublicID(ctx, old); err != nil {
		t.Fatalf("5 s before the end: %v", err)
	}
	clk.t = clk.t.Add(6 * time.Second)
	if _, err := dir.ByPublicID(ctx, old); errCode(err) != contract.CodeWorkspaceURLReset {
		t.Fatalf("past grace must be %s, got %v", contract.CodeWorkspaceURLReset, err)
	}
	if _, err := dir.ByPublicID(ctx, "zzzzzzzzzzzzzzzzzzzzzz"); errCode(err) != contract.CodeNotFound {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestRetirePreviousEndsGraceAtOnceAndSweepDropsDeadEntries(t *testing.T) {
	dir, svc, _, clk := newDir(t)
	ctx := context.Background()
	ws, _ := svc.CreateWorkspace(ctx, principal(), "Alpha")
	old := ws.PublicID
	if _, err := dir.Reset(ctx, principal(), ws.ID, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.ByPublicID(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := dir.RetirePrevious(ctx, principal(), ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.ByPublicID(ctx, old); errCode(err) != contract.CodeWorkspaceURLReset {
		t.Fatalf("after retire-previous: %v", err)
	}
	if err := dir.RetirePrevious(ctx, principal(), "00000000-0000-0000-0000-000000000fff"); errCode(err) != contract.CodeNotFound {
		t.Fatalf("unknown workspace: %v", err)
	}
	clk.t = clk.t.Add(2 * tenancy.DirectoryTTL)
	if n := dir.SweepRetired(clk.t); n < 1 {
		t.Fatalf("sweep dropped %d, want the expired reset entry at least", n)
	}
	if n := dir.SweepRetired(clk.t); n != 0 {
		t.Fatalf("second sweep dropped %d", n)
	}
}
