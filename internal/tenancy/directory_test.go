package tenancy_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
	"github.com/ethosagent/meterwise/internal/tenancy"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newDir(t testing.TB) (*tenancy.Directory, *tenancy.Service, *store.Store, *clock) {
	t.Helper()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	clk := &clock{t: time.Now()}
	svc := tenancy.New(db, identity.Disabled{}, tenancy.Config{MaxWorkspaces: 5, Now: clk.now})
	if err := svc.EnsureDefaults(context.Background()); err != nil {
		t.Fatal(err)
	}
	return svc.Directory(), svc, db, clk
}

func principal() contract.Principal {
	return contract.Principal{UserID: "00000000-0000-0000-0000-0000000000cc", OrgID: store.OrgID(), Role: contract.RoleAdmin}
}

func TestDirectoryReturnsThePlan05TupleAndCachesFor30Seconds(t *testing.T) {
	dir, svc, db, clk := newDir(t)
	ctx := context.Background()
	ws, err := svc.CreateWorkspace(ctx, principal(), "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	got, err := dir.ByPublicID(ctx, ws.PublicID)
	if err != nil || got.ID != ws.ID || got.OrgID != store.OrgID() || got.Settings == nil || got.Retired || got.Name != "Alpha" {
		t.Fatalf("ByPublicID = %+v, %v", got, err)
	}
	byID, err := dir.ByID(ctx, ws.ID)
	if err != nil || byID.PublicID != ws.PublicID {
		t.Fatalf("ByID = %+v, %v", byID, err)
	}

	// change the row behind the cache's back: the cached copy is served for 30 s only
	if _, err := db.Pool().Exec(ctx, `UPDATE workspaces SET name = 'Changed' WHERE id = $1`, ws.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = dir.ByPublicID(ctx, ws.PublicID); got.Name != "Alpha" {
		t.Fatalf("within the TTL the cached name is expected, got %q", got.Name)
	}
	clk.t = clk.t.Add(tenancy.DirectoryTTL - time.Second)
	if got, _ = dir.ByPublicID(ctx, ws.PublicID); got.Name != "Alpha" {
		t.Fatalf("just inside the TTL: %q", got.Name)
	}
	clk.t = clk.t.Add(2 * time.Second)
	if got, _ = dir.ByPublicID(ctx, ws.PublicID); got.Name != "Changed" {
		t.Fatalf("after the TTL the row must be re-read, got %q", got.Name)
	}
}

func TestDirectoryIsBustedOnUpdateAndDelete(t *testing.T) {
	dir, svc, _, _ := newDir(t)
	ctx := context.Background()
	ws, _ := svc.CreateWorkspace(ctx, principal(), "Alpha")
	_, _ = dir.ByPublicID(ctx, ws.PublicID)
	_, _ = dir.ByID(ctx, ws.ID)

	name := "Renamed"
	if _, err := svc.UpdateWorkspace(ctx, principal(), ws.ID, contract.UpdateWorkspaceRequest{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if got, _ := dir.ByPublicID(ctx, ws.PublicID); got.Name != "Renamed" {
		t.Fatalf("update did not bust ByPublicID: %q", got.Name)
	}
	if got, _ := dir.ByID(ctx, ws.ID); got.Name != "Renamed" {
		t.Fatalf("update did not bust ByID: %q", got.Name)
	}

	if err := svc.DeleteWorkspace(ctx, principal(), ws.ID); err != nil {
		t.Fatal(err)
	}
	for name, lookup := range map[string]func() error{
		"ByPublicID": func() error { _, err := dir.ByPublicID(ctx, ws.PublicID); return err },
		"ByID":       func() error { _, err := dir.ByID(ctx, ws.ID); return err },
	} {
		if e := contract.AsError(lookup()); e.Code != contract.CodeNotFound {
			t.Errorf("%s after delete: %s, want not_found (a cached copy survived)", name, e.Code)
		}
	}
}

func TestDirectoryUnknownIdsAreNotFound(t *testing.T) {
	dir, _, _, _ := newDir(t)
	for _, id := range []string{"ZZZZZZZZZZZZZZZZZZZZZZ", "short", "", "' OR '1'='1"} {
		if _, err := dir.ByPublicID(context.Background(), id); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("ByPublicID(%q) = %v", id, err)
		}
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000042", "nope"} {
		if _, err := dir.ByID(context.Background(), id); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("ByID(%q) = %v", id, err)
		}
	}
}

func TestRetiredPublicIDResolvesWithRetiredInsideItsGraceWindow(t *testing.T) {
	dir, svc, db, clk := newDir(t)
	ctx := context.Background()
	ws, _ := svc.CreateWorkspace(ctx, principal(), "Alpha")
	// what Plan 04's reset writes: a retired row for the old id, a new current id
	for _, stmt := range []string{
		`UPDATE workspace_public_ids SET retired_at = now(), grace_until = now() + interval '1 hour' WHERE workspace_id = $1`,
		`UPDATE workspaces SET public_id = 'NEWnewNEWnewNEWnewNEWn' WHERE id = $1`,
		`INSERT INTO workspace_public_ids (public_id, org_id, workspace_id) VALUES ('NEWnewNEWnewNEWnewNEWn', '` + store.OrgID() + `', $1)`,
	} {
		if _, err := db.Pool().Exec(ctx, stmt, ws.ID); err != nil {
			t.Fatal(err)
		}
	}
	old, err := dir.ByPublicID(ctx, ws.PublicID)
	if err != nil || !old.Retired || old.ID != ws.ID || old.OrgID != store.OrgID() {
		t.Fatalf("retired id = %+v, %v", old, err)
	}
	cur, err := dir.ByPublicID(ctx, "NEWnewNEWnewNEWnewNEWn")
	if err != nil || cur.Retired || cur.ID != ws.ID {
		t.Fatalf("current id = %+v, %v", cur, err)
	}
	// past the grace window the old id is a reset URL (410), not an unknown one
	clk.t = clk.t.Add(3 * time.Hour)
	if _, err := dir.ByPublicID(ctx, ws.PublicID); contract.AsError(err).Code != contract.CodeWorkspaceURLReset {
		t.Fatalf("after the grace window: %v", err)
	}
}

func TestSwitchRejectsAWorkspaceOutsideTheOrg(t *testing.T) {
	_, svc, _, _ := newDir(t)
	if _, err := svc.SwitchWorkspace(context.Background(), principal(), "00000000-0000-0000-0000-000000000042"); contract.AsError(err).Code != contract.CodeNotFound {
		t.Fatalf("got %v", err)
	}
}

func loads(d *tenancy.Directory) uint64 { return d.Status()["loads"].(uint64) }

func TestConcurrentMissesOnOneIDShareOnePostgresRead(t *testing.T) {
	dir, svc, _, _ := newDir(t)
	ctx := context.Background()
	ws, err := svc.CreateWorkspace(ctx, principal(), "Alpha")
	if err != nil {
		t.Fatal(err)
	}
	before := loads(dir)
	var wg sync.WaitGroup
	for range 60 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got, err := dir.ByPublicID(ctx, ws.PublicID); err != nil || got.ID != ws.ID {
				t.Errorf("ByPublicID = %+v, %v", got, err)
			}
		}()
	}
	wg.Wait()
	if n := loads(dir) - before; n != 1 {
		t.Fatalf("60 concurrent misses made %d Postgres reads, want 1", n)
	}
}

func TestUnknownIDsAreRememberedFor5SecondsInABoundedCache(t *testing.T) {
	dir, _, _, clk := newDir(t)
	ctx := context.Background()
	const id = "ZZZZZZZZZZZZZZZZZZZZZZ"
	for range 3 {
		if _, err := dir.ByPublicID(ctx, id); contract.AsError(err).Code != contract.CodeNotFound {
			t.Fatalf("ByPublicID = %v", err)
		}
	}
	if n := loads(dir); n != 1 {
		t.Fatalf("3 lookups of one unknown id made %d reads, want 1", n)
	}
	clk.t = clk.t.Add(6 * time.Second)
	_, _ = dir.ByPublicID(ctx, id)
	if n := loads(dir); n != 2 {
		t.Fatalf("after the 5 s the id must be asked again: %d reads", n)
	}
	for i := range 1200 {
		_, _ = dir.ByPublicID(ctx, fmt.Sprintf("%022d", i))
	}
	if n := dir.Status()["negative_entries"].(int); n > 1024 {
		t.Fatalf("negative cache holds %d entries, cap is 1024", n)
	}
}

func TestCachedViewServesHitsWithoutPostgresAndFollowsBust(t *testing.T) {
	dir, svc, _, _ := newDir(t)
	ctx := context.Background()
	ws, _ := svc.CreateWorkspace(ctx, principal(), "Alpha")
	dir.SetView(func(w contract.Workspace) any { return "view:" + w.Name })
	if _, ok, _ := dir.CachedView(ws.PublicID); ok {
		t.Fatal("a cold cache answered")
	}
	v, err := dir.LoadView(ctx, ws.PublicID)
	if err != nil || v != "view:Alpha" {
		t.Fatalf("LoadView = %v, %v", v, err)
	}
	before := loads(dir)
	if v, ok, err := dir.CachedView(ws.PublicID); !ok || err != nil || v != "view:Alpha" {
		t.Fatalf("CachedView = %v, %v, %v", v, ok, err)
	}
	if loads(dir) != before {
		t.Fatal("a hit read Postgres")
	}
	dir.Bust(ws.ID)
	if _, ok, _ := dir.CachedView(ws.PublicID); ok {
		t.Fatal("a busted entry still answers")
	}
	if _, err := dir.LoadView(ctx, "ZZZZZZZZZZZZZZZZZZZZZZ"); contract.AsError(err).Code != contract.CodeNotFound {
		t.Fatalf("unknown id: %v", err)
	}
	if _, ok, err := dir.CachedView("ZZZZZZZZZZZZZZZZZZZZZZ"); !ok || contract.AsError(err).Code != contract.CodeNotFound {
		t.Fatalf("a remembered unknown id must answer from the cache: ok=%v err=%v", ok, err)
	}
}
