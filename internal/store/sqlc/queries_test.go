package sqlc_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

// TestEveryGeneratedQueryRunsAgainstPostgres walks the whole store API, which is the
// only caller of the generated queries, so a column or cast that drifts from the
// migrations fails here and not in production.
func TestEveryGeneratedQueryRunsAgainstPostgres(t *testing.T) {
	ctx, org, now := context.Background(), store.OrgID(), time.Now()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(ctx, dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.Ping(ctx))
	if _, err := s.Org(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_q", Email: "q@example.test", Name: "Q"})
	must(err)
	_, _, err = s.MergePreferences(ctx, org, u.ID, map[string]any{"theme": "dark"})
	must(err)
	_, _, err = s.Preferences(ctx, org, u.ID)
	must(err)
	inv, err := s.CreateInvitation(ctx, store.NewInvitation{OrgID: org, Email: "q@example.test", Role: contract.RoleMember, ExpiresAt: now.Add(time.Hour), Now: now})
	must(err)
	_, err = s.ListInvitations(ctx, org)
	must(err)
	_, err = s.Invitation(ctx, org, inv.ID)
	must(err)
	_, err = s.OpenInvitation(ctx, org, "q@example.test", now)
	must(err)
	must(s.RefreshInvitation(ctx, org, inv.ID, "inv_q", now.Add(2*time.Hour)))
	_, ok, err := s.AcceptOpenInvitation(ctx, org, u.ID, "q@example.test", now)
	must(err)
	if !ok {
		t.Fatal("invitation not accepted")
	}
	_, _, _, err = s.Membership(ctx, org, u.ID)
	must(err)
	must(s.SetMembershipStatus(ctx, org, u.ID, "removed"))
	must(s.UpsertActiveMembership(ctx, org, u.ID, contract.RoleAdmin))
	_, err = s.EnsureDefaultWorkspace(ctx, org, "Default", "AAAAAAAAAAAAAAAAAAAAAA")
	must(err)
	ws, err := s.CreateWorkspace(ctx, org, "W", "BBBBBBBBBBBBBBBBBBBBBB", 5)
	must(err)
	name := "W2"
	_, err = s.UpdateWorkspace(ctx, org, ws.ID, &name, map[string]any{"k": 1})
	must(err)
	_, err = s.ListWorkspaces(ctx, org)
	must(err)
	_, err = s.Workspace(ctx, org, ws.ID)
	must(err)
	_, err = s.WorkspaceByID(ctx, ws.ID)
	must(err)
	_, err = s.WorkspaceByPublicID(ctx, ws.PublicID, now)
	must(err)
	must(s.DeleteWorkspace(ctx, org, ws.ID))
	other, err := s.CreateInvitation(ctx, store.NewInvitation{OrgID: org, Email: "r@example.test", Role: contract.RoleMember, ExpiresAt: now.Add(time.Hour), Now: now})
	must(err)
	_, err = s.RevokeInvitation(ctx, org, other.ID, now)
	must(err)
	must(s.SoftDeleteUser(ctx, org, "user_q"))
}
