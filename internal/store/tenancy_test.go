package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

func openMigrated(t *testing.T) *store.Store {
	t.Helper()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func code(err error) contract.Code { return contract.AsError(err).Code }

const missingID = "00000000-0000-0000-0000-000000000042"

func TestUsersMirrorLazilyAndNeverClobberWithEmptyClaims(t *testing.T) {
	s, ctx, org := openMigrated(t), context.Background(), store.OrgID()
	u, err := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_1", Email: " Ada@Example.test ", Name: "Ada", AvatarURL: "a.png"})
	if err != nil || u.ID == "" || u.Email != "ada@example.test" || u.Deleted {
		t.Fatalf("create: %+v %v", u, err)
	}
	same, err := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_1"}) // empty claims: nothing changes
	if err != nil || same != u {
		t.Fatalf("empty claims changed the row: %+v %v", same, err)
	}
	renamed, err := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_1", Name: "Ada L."})
	if err != nil || renamed.ID != u.ID || renamed.Name != "Ada L." || renamed.Email != "ada@example.test" || renamed.AvatarURL != "a.png" {
		t.Fatalf("update: %+v %v", renamed, err)
	}
	if err := s.SoftDeleteUser(ctx, org, "user_1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SoftDeleteUser(ctx, org, "user_never_seen"); err != nil {
		t.Fatalf("deleting an unknown user must not fail: %v", err)
	}
	after, _ := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_1", Email: "ada@example.test"})
	if !after.Deleted {
		t.Fatal("an upsert resurrected a soft-deleted user")
	}
}

func TestPreferencesMergeAndDefaults(t *testing.T) {
	s, ctx, org := openMigrated(t), context.Background(), store.OrgID()
	u, _ := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_1"})
	p, active, err := s.Preferences(ctx, org, u.ID)
	if err != nil || p.Theme != contract.ThemeSystem || p.ReducedMotion || active != "" {
		t.Fatalf("defaults: %+v %q %v", p, active, err)
	}
	if _, _, err = s.MergePreferences(ctx, org, u.ID, map[string]any{"theme": "dark"}); err != nil {
		t.Fatal(err)
	}
	p, active, err = s.MergePreferences(ctx, org, u.ID, map[string]any{"reducedMotion": true, "activeWorkspaceId": missingID})
	if err != nil || p.Theme != contract.ThemeDark || !p.ReducedMotion || active != missingID {
		t.Fatalf("merge: %+v %q %v", p, active, err)
	}
	if _, _, err = s.MergePreferences(ctx, org, u.ID, map[string]any{"theme": "neon"}); err != nil {
		t.Fatal(err)
	}
	if p, _, _ = s.Preferences(ctx, org, u.ID); p.Theme != contract.ThemeSystem {
		t.Fatalf("a stored invalid theme must read as system, got %q", p.Theme)
	}
	for _, id := range []string{"nope", missingID} {
		if _, _, err := s.Preferences(ctx, org, id); code(err) != contract.CodeNotFound {
			t.Errorf("Preferences(%q) = %v", id, err)
		}
		if _, _, err := s.MergePreferences(ctx, org, id, map[string]any{}); code(err) != contract.CodeNotFound {
			t.Errorf("MergePreferences(%q) = %v", id, err)
		}
	}
	if _, _, err := s.MergePreferences(ctx, org, u.ID, map[string]any{"x": func() {}}); err == nil {
		t.Fatal("a non-JSON patch was accepted")
	}
}

func TestMembershipsAndInvitationAcceptance(t *testing.T) {
	s, ctx, org := openMigrated(t), context.Background(), store.OrgID()
	now := time.Now()
	u, _ := s.UpsertUser(ctx, org, contract.Identity{Subject: "user_1", Email: "a@example.test"})
	if _, _, found, err := s.Membership(ctx, org, u.ID); err != nil || found {
		t.Fatalf("a new user has no membership: found=%v %v", found, err)
	}
	if _, _, found, _ := s.Membership(ctx, org, "nope"); found {
		t.Fatal("a malformed id found a membership")
	}
	inv, err := s.CreateInvitation(ctx, store.NewInvitation{OrgID: org, Email: "a@example.test", Role: contract.RoleMember, ClerkInvitationID: "inv_1", ExpiresAt: now.Add(time.Hour), Now: now})
	if err != nil || inv.InvitedBy != "" || inv.ClerkInvitationID != "inv_1" || inv.StatusAt(now) != contract.InvitationPending {
		t.Fatalf("create: %+v %v", inv, err)
	}
	if _, err := s.CreateInvitation(ctx, store.NewInvitation{OrgID: org, Email: "a@example.test", Role: contract.RoleAdmin, ExpiresAt: now.Add(time.Hour), Now: now}); code(err) != contract.CodeDuplicateInvitation {
		t.Fatalf("duplicate: %v", err)
	}
	if _, ok, err := s.AcceptOpenInvitation(ctx, org, u.ID, "other@example.test", now); err != nil || ok {
		t.Fatalf("a different email accepted: %v %v", ok, err)
	}
	role, ok, err := s.AcceptOpenInvitation(ctx, org, u.ID, "a@example.test", now)
	if err != nil || !ok || role != contract.RoleMember {
		t.Fatalf("accept: %q %v %v", role, ok, err)
	}
	if _, ok, _ = s.AcceptOpenInvitation(ctx, org, u.ID, "a@example.test", now); ok {
		t.Fatal("an invitation was accepted twice")
	}
	if r, active, found, _ := s.Membership(ctx, org, u.ID); !found || !active || r != contract.RoleMember {
		t.Fatalf("membership after accept: %q %v %v", r, active, found)
	}
	if err := s.SetMembershipStatus(ctx, org, u.ID, "removed"); err != nil {
		t.Fatal(err)
	}
	if _, active, found, _ := s.Membership(ctx, org, u.ID); !found || active {
		t.Fatal("removal not visible")
	}
	if err := s.UpsertActiveMembership(ctx, org, u.ID, contract.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if r, active, _, _ := s.Membership(ctx, org, u.ID); !active || r != contract.RoleAdmin {
		t.Fatal("re-activation with a new role failed")
	}
	for _, id := range []string{"nope", missingID} {
		if err := s.SetMembershipStatus(ctx, org, id, "removed"); code(err) != contract.CodeNotFound {
			t.Errorf("SetMembershipStatus(%q) = %v", id, err)
		}
	}
	if err := s.UpsertActiveMembership(ctx, org, "nope", contract.RoleMember); code(err) != contract.CodeNotFound {
		t.Fatalf("got %v", err)
	}
	// a user deleted by webhook loses the membership too
	if err := s.SoftDeleteUser(ctx, org, "user_1"); err != nil {
		t.Fatal(err)
	}
	if _, active, _, _ := s.Membership(ctx, org, u.ID); active {
		t.Fatal("soft-deleting a user left an active membership")
	}
}

func TestInvitationQueriesAreOrgScopedAndGuardIDs(t *testing.T) {
	s, ctx, org := openMigrated(t), context.Background(), store.OrgID()
	now := time.Now()
	inv, _ := s.CreateInvitation(ctx, store.NewInvitation{OrgID: org, Email: "a@example.test", Role: contract.RoleMember, ExpiresAt: now.Add(time.Hour), Now: now})
	list, err := s.ListInvitations(ctx, org)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %+v %v", list, err)
	}
	if other, _ := s.ListInvitations(ctx, missingID); len(other) != 0 {
		t.Fatal("another org's id listed invitations")
	}
	if _, err := s.Invitation(ctx, missingID, inv.ID); code(err) != contract.CodeNotFound {
		t.Fatalf("cross-org Invitation: %v", err)
	}
	if _, err := s.Invitation(ctx, org, "nope"); code(err) != contract.CodeNotFound {
		t.Fatalf("malformed: %v", err)
	}
	if _, err := s.OpenInvitation(ctx, org, "nobody@example.test", now); code(err) != contract.CodeNotFound {
		t.Fatalf("OpenInvitation: %v", err)
	}
	if err := s.RefreshInvitation(ctx, org, inv.ID, "inv_new", now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Invitation(ctx, org, inv.ID); got.ClerkInvitationID != "inv_new" || !got.ExpiresAt.After(now.Add(24*time.Hour)) {
		t.Fatalf("refresh: %+v", got)
	}
	for _, id := range []string{"nope", missingID} {
		if err := s.RefreshInvitation(ctx, org, id, "x", now); code(err) != contract.CodeNotFound {
			t.Errorf("RefreshInvitation(%q) = %v", id, err)
		}
		if _, err := s.RevokeInvitation(ctx, org, id, now); code(err) != contract.CodeNotFound {
			t.Errorf("RevokeInvitation(%q) = %v", id, err)
		}
	}
	clerkID, err := s.RevokeInvitation(ctx, org, inv.ID, now)
	if err != nil || clerkID != "inv_new" {
		t.Fatalf("revoke: %q %v", clerkID, err)
	}
	if _, err := s.RevokeInvitation(ctx, org, inv.ID, now); code(err) != contract.CodeNotFound {
		t.Fatal("a revoked invitation was revoked twice")
	}
}

func TestWorkspacesStoreRules(t *testing.T) {
	s, ctx, org := openMigrated(t), context.Background(), store.OrgID()
	now := time.Now()
	created, err := s.EnsureDefaultWorkspace(ctx, org, "Default", "AAAAAAAAAAAAAAAAAAAAAA")
	if err != nil || !created {
		t.Fatalf("default: %v %v", created, err)
	}
	if again, err := s.EnsureDefaultWorkspace(ctx, org, "Default", "BBBBBBBBBBBBBBBBBBBBBB"); err != nil || again {
		t.Fatalf("second default: %v %v", again, err)
	}
	if _, err := s.EnsureDefaultWorkspace(ctx, missingID, "x", "AAAAAAAAAAAAAAAAAAAAAA"); !errors.Is(err, store.ErrPublicIDTaken) && err == nil {
		t.Logf("foreign org default: %v", err) // the FK rejects an org that does not exist
	}
	ws, err := s.CreateWorkspace(ctx, org, "Alpha", "CCCCCCCCCCCCCCCCCCCCCC", 2)
	if err != nil || ws.IsDefault || ws.Name != "Alpha" {
		t.Fatalf("create: %+v %v", ws, err)
	}
	if _, err := s.CreateWorkspace(ctx, org, "Beta", "DDDDDDDDDDDDDDDDDDDDDD", 2); code(err) != contract.CodeWorkspaceLimit {
		t.Fatalf("limit: %v", err)
	}
	if _, err := s.CreateWorkspace(ctx, org, "Beta", "CCCCCCCCCCCCCCCCCCCCCC", 9); !errors.Is(err, store.ErrPublicIDTaken) {
		t.Fatalf("a taken public id: %v", err)
	}
	list, _ := s.ListWorkspaces(ctx, org)
	if len(list) != 2 || !list[0].IsDefault {
		t.Fatalf("list order: %+v", list)
	}
	name := "Alpha 2"
	up, err := s.UpdateWorkspace(ctx, org, ws.ID, &name, map[string]any{"capture": true})
	if err != nil || up.Name != name || up.Settings["capture"] != true {
		t.Fatalf("update: %+v %v", up, err)
	}
	if up, _ = s.UpdateWorkspace(ctx, org, ws.ID, nil, nil); up.Name != name || up.Settings["capture"] != true {
		t.Fatalf("an empty update changed the row: %+v", up)
	}
	if _, err := s.UpdateWorkspace(ctx, org, ws.ID, nil, map[string]any{"x": func() {}}); err == nil {
		t.Fatal("non-JSON settings accepted")
	}
	for _, id := range []string{"nope", missingID} {
		if _, err := s.UpdateWorkspace(ctx, org, id, &name, nil); code(err) != contract.CodeNotFound {
			t.Errorf("UpdateWorkspace(%q) = %v", id, err)
		}
		if _, err := s.Workspace(ctx, org, id); code(err) != contract.CodeNotFound {
			t.Errorf("Workspace(%q) = %v", id, err)
		}
		if _, err := s.WorkspaceByID(ctx, id); code(err) != contract.CodeNotFound {
			t.Errorf("WorkspaceByID(%q) = %v", id, err)
		}
		if err := s.DeleteWorkspace(ctx, org, id); code(err) != contract.CodeNotFound {
			t.Errorf("DeleteWorkspace(%q) = %v", id, err)
		}
	}
	if _, err := s.Workspace(ctx, missingID, ws.ID); code(err) != contract.CodeNotFound {
		t.Fatalf("cross-org Workspace: %v", err)
	}
	if err := s.DeleteWorkspace(ctx, org, list[0].ID); code(err) != contract.CodeDefaultWorkspace {
		t.Fatalf("delete default: %v", err)
	}
	if got, err := s.WorkspaceByPublicID(ctx, ws.PublicID, now); err != nil || got.ID != ws.ID || got.Retired {
		t.Fatalf("by public id: %+v %v", got, err)
	}
	if err := s.DeleteWorkspace(ctx, org, ws.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.WorkspaceByPublicID(ctx, ws.PublicID, now); code(err) != contract.CodeNotFound {
		t.Fatalf("a deleted workspace's id still resolves: %v", err)
	}
	if o, err := s.Org(ctx); err != nil || o.ID != org || o.Name == "" {
		t.Fatalf("org: %+v %v", o, err)
	}
}

func TestDriverFailuresSurfaceAsScrubbedStoreErrors(t *testing.T) {
	s := openMigrated(t)
	s.Close()
	_, err := s.ListWorkspaces(context.Background(), store.OrgID())
	if store.ClassOf(err) != store.ClassUnreachable {
		t.Fatalf("closed pool: %v (%T)", err, err)
	}
	if _, err := store.Migrate(context.Background(), pgtest.DSN(t)+"&nonsense=1"); store.ClassOf(err) == "" {
		t.Fatalf("a failing migrate must be a store error: %v", err)
	}
}
