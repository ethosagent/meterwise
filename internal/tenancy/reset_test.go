package tenancy_test

import (
	"context"
	"sync"
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

// A lookup that read Postgres before a reset must not put its stale row in the cache after the
// reset's Bust: the old id would then authenticate as current until the TTL ran out.
func TestALookupRacingAResetNeverRecachesTheOldID(t *testing.T) {
	dir, svc, _, _ := newDir(t)
	ctx := context.Background()
	ws, err := svc.CreateWorkspace(ctx, principal(), "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	current := ws.PublicID
	for round := range 60 {
		old := current
		stop := make(chan struct{})
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stop:
						return
					default:
					}
					dir.Bust(ws.ID)
					_, _ = dir.ByPublicID(ctx, old)
				}
			}()
		}
		res, err := dir.Reset(ctx, principal(), ws.ID, 0)
		close(stop)
		wg.Wait()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := dir.ByPublicID(ctx, old); errCode(err) != contract.CodeWorkspaceURLReset {
			t.Fatalf("round %d: the old id (grace 0) must be %s, got %v", round, contract.CodeWorkspaceURLReset, err)
		}
		current = res.Workspace.PublicID
	}
}
