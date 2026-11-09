package store_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

var readSeed = []store.GrantSpec{
	{ResourceType: "workspace", Action: "read"}, {ResourceType: "workspace_url", Action: "read"},
	{ResourceType: "session", Action: "read"}, {ResourceType: "monitor_config", Action: "read"},
	{ResourceType: "savings", Action: "read"},
}

func groupRig(t *testing.T) (*store.Store, string, contract.Group, contract.Workspace) {
	t.Helper()
	ctx, org := context.Background(), store.OrgID()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	g, err := s.CreateGroup(ctx, org, "Readers")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := s.CreateWorkspace(ctx, org, "Alpha", "CCCCCCCCCCCCCCCCCCCCCC", 9)
	if err != nil {
		t.Fatal(err)
	}
	return s, dsn, g, ws
}

// Attaching a group that is already attached changes nothing: it must not bring back a grant an
// admin removed with PUT /permissions.
func TestReattachingAnAttachedGroupDoesNotReseedRemovedGrants(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, _, g, ws := groupRig(t)
	if _, err := s.AttachGroupWorkspace(ctx, org, g.ID, ws.ID, readSeed); err != nil {
		t.Fatal(err)
	}
	only := []contract.Grant{{WorkspaceID: &ws.ID, ResourceType: "session", Action: "read"}}
	if _, err := s.ReplaceGroupGrants(ctx, org, g.ID, only); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AttachGroupWorkspace(ctx, org, g.ID, ws.ID, readSeed); err != nil {
		t.Fatal(err)
	}
	got, err := s.GroupGrants(ctx, org, g.ID)
	if err != nil || len(got) != 1 || got[0].ResourceType != "session" {
		t.Fatalf("re-attach re-seeded removed grants: %+v %v", got, err)
	}
}

// PUT permissions racing a detach must not leave a grant on a workspace the group is no longer
// attached to: a later attach would resurrect it.
func TestReplaceGrantsRacingDetachLeavesNoOrphanGrant(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, dsn, g, ws := groupRig(t)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	grants := []contract.Grant{
		{WorkspaceID: &ws.ID, ResourceType: "workspace", Action: "manage"},
		{WorkspaceID: &ws.ID, ResourceType: "session", Action: "read"},
	}
	orphans := 0
	for round := 0; round < 40; round++ {
		if _, err := s.AttachGroupWorkspace(ctx, org, g.ID, ws.ID, readSeed); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _, _ = s.ReplaceGroupGrants(ctx, org, g.ID, grants) }()
		go func() { defer wg.Done(); _ = s.DetachGroupWorkspace(ctx, org, g.ID, ws.ID) }()
		wg.Wait()
		var n int
		if err := conn.QueryRow(ctx, `SELECT count(*) FROM group_permissions gp JOIN resource_permissions rp ON rp.id = gp.permission_id
WHERE gp.group_id = $1 AND NOT EXISTS (SELECT 1 FROM group_workspaces gw WHERE gw.group_id = gp.group_id AND gw.workspace_id = rp.workspace_id)`, g.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			orphans++
		}
		_ = s.DetachGroupWorkspace(ctx, org, g.ID, ws.ID) // reset for the next round
		if _, err := conn.Exec(ctx, `DELETE FROM group_permissions WHERE group_id = $1`, g.ID); err != nil {
			t.Fatal(err)
		}
	}
	if orphans > 0 {
		t.Fatalf("%d of 40 rounds left grants on a detached workspace", orphans)
	}
}
