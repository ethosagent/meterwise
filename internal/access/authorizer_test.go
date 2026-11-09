package access_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
)

type fakeRepo struct {
	grants []access.Grant
	err    error
	calls  int
}

func (f *fakeRepo) Grants(context.Context, string, string) ([]access.Grant, error) {
	f.calls++
	return f.grants, f.err
}

type fakeWS map[string]bool

func (f fakeWS) Workspace(_ context.Context, _, id string) (contract.Workspace, error) {
	if f[id] {
		return contract.Workspace{ID: id}, nil
	}
	return contract.Workspace{}, contract.NewError(contract.CodeNotFound, "not found")
}

func principal(role contract.Role) contract.Principal {
	return contract.Principal{UserID: "u", OrgID: "o", Role: role}
}

func TestAuthorizerLoadsGrantsOnlyWhenTheyMatter(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{grants: []access.Grant{{GroupID: "g1", WorkspaceID: wsA, ResourceType: "workspace", Action: "write"}}}
	a := &access.Authorizer{Repo: repo, Workspaces: fakeWS{wsA: true, wsB: true}}

	for _, tc := range []struct {
		name  string
		role  contract.Role
		perm  contract.Permission
		ws    string
		want  access.Result
		loads int
	}{
		{"admin: no grant read", "admin", contract.PermWorkspaceManage, wsA, access.Result{Allowed: true, Reason: "admin"}, 0},
		{"member on PermMember: no grant read", "member", contract.PermMember, "", access.Result{Allowed: true, Reason: "member"}, 0},
		{"member on PermAdmin: no grant read", "member", contract.PermAdmin, "", access.Result{Reason: "admin required"}, 0},
		{"member allowed: one grant read", "member", contract.PermWorkspaceWrite, wsA, access.Result{Allowed: true, Reason: "group:g1"}, 1},
		{"member denied on the other workspace: one grant read", "member", contract.PermWorkspaceWrite, wsB, access.Result{Reason: "no matching grant"}, 1},
	} {
		repo.calls = 0
		got, err := a.Authorize(ctx, principal(tc.role), tc.perm, tc.ws)
		if err != nil || got != tc.want || repo.calls != tc.loads {
			t.Errorf("%s: %+v err=%v loads=%d", tc.name, got, err, repo.calls)
		}
	}
}

func TestAuthorizerAnswersNotFoundForAForeignWorkspaceBeforeAnyGrant(t *testing.T) {
	repo := &fakeRepo{}
	a := &access.Authorizer{Repo: repo, Workspaces: fakeWS{}}
	for _, role := range []contract.Role{"admin", "member"} {
		_, err := a.Authorize(context.Background(), principal(role), contract.PermWorkspaceRead, wsA)
		if contract.AsError(err).Code != contract.CodeNotFound || repo.calls != 0 {
			t.Fatalf("%s: %v loads=%d", role, err, repo.calls)
		}
	}
}

func TestAuthorizerSurfacesAStoreFailureAsAnErrorNeverADenial(t *testing.T) {
	boom := errors.New("db down")
	a := &access.Authorizer{Repo: &fakeRepo{err: boom}, Workspaces: fakeWS{wsA: true}}
	res, err := a.Authorize(context.Background(), principal("member"), contract.PermWorkspaceRead, wsA)
	if !errors.Is(err, boom) || res.Allowed {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := a.Snapshot(context.Background(), principal("member")); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := a.Filter(context.Background(), principal("member"), contract.PermWorkspaceRead, []string{wsA}); !errors.Is(err, boom) {
		t.Fatal(err)
	}
}

func TestFilterKeepsTheReadableWorkspacesInOrder(t *testing.T) {
	repo := &fakeRepo{grants: []access.Grant{{GroupID: "g", WorkspaceID: wsB, ResourceType: "workspace", Action: "read"}}}
	a := &access.Authorizer{Repo: repo, Workspaces: fakeWS{}}
	got, err := a.Filter(context.Background(), principal("member"), contract.PermWorkspaceRead, []string{wsA, wsB})
	if err != nil || len(got) != 1 || got[0] != wsB || repo.calls != 1 {
		t.Fatalf("%v %v loads=%d", got, err, repo.calls)
	}
	repo.calls = 0
	all, _ := a.Filter(context.Background(), principal("admin"), contract.PermWorkspaceRead, []string{wsA, wsB})
	if len(all) != 2 || repo.calls != 0 {
		t.Fatalf("%v loads=%d", all, repo.calls)
	}
}

func TestSnapshotReadsGrantsOnlyForAMember(t *testing.T) {
	repo := &fakeRepo{grants: []access.Grant{{ResourceType: "savings", Action: "read"}}}
	a := &access.Authorizer{Repo: repo, Workspaces: fakeWS{}}
	snap, err := a.Snapshot(context.Background(), principal("admin"))
	if err != nil || !snap.Admin || repo.calls != 0 {
		t.Fatalf("%+v %v %d", snap, err, repo.calls)
	}
	snap, _ = a.Snapshot(context.Background(), principal("member"))
	if snap.Admin || len(snap.Grants) != 1 || repo.calls != 1 {
		t.Fatalf("%+v %d", snap, repo.calls)
	}
}
