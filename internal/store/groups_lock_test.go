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

var readSeed = []contract.GrantSpec{
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

// Delete racing attach: every call ends as success or not_found (never a driver error from a
// foreign-key race), and no link, grant or membership row outlives the group.
func TestDeleteRacingAttachLeavesNoOrphanAndNoDriverError(t *testing.T) {
	ctx, org := context.Background(), store.OrgID()
	s, dsn, _, ws := groupRig(t)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	for round := 0; round < 40; round++ {
		g, err := s.CreateGroup(ctx, org, "Race"+string(rune('A'+round%26))+string(rune('a'+round/26)))
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make([]error, 3)
		wg.Add(3)
		go func() { defer wg.Done(); _, errs[0] = s.AttachGroupWorkspace(ctx, org, g.ID, ws.ID, readSeed) }()
		go func() { defer wg.Done(); errs[1] = s.DeleteGroup(ctx, org, g.ID) }()
		go func() { defer wg.Done(); _, errs[2] = s.RenameGroup(ctx, org, g.ID, "Renamed"+g.ID[:8]) }()
		wg.Wait()
		for i, e := range errs {
			if e != nil && contract.AsError(e).Code != contract.CodeNotFound {
				t.Fatalf("round %d call %d: %v", round, i, e)
			}
		}
		var n int
		if err := conn.QueryRow(ctx, `SELECT (SELECT count(*) FROM group_workspaces WHERE group_id = $1)
 + (SELECT count(*) FROM group_permissions WHERE group_id = $1) + (SELECT count(*) FROM group_users WHERE group_id = $1)
 WHERE NOT EXISTS (SELECT 1 FROM groups WHERE id = $1)`, g.ID).Scan(&n); err != nil && err != pgx.ErrNoRows {
			t.Fatal(err)
		}
		if n > 0 {
			t.Fatalf("round %d: %d rows outlived the deleted group", round, n)
		}
	}
}
