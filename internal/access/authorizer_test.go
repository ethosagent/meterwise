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

// fakeWS maps a workspace id to its org; it serves the data-plane directory's ByID, which is not
// scoped to an org (the Authorizer checks the org itself).
type fakeWS map[string]string

func (f fakeWS) ByID(_ context.Context, id string) (contract.Workspace, error) {
	if org, ok := f[id]; ok {
		return contract.Workspace{ID: id, OrgID: org}, nil
	}
	return contract.Workspace{}, contract.NewError(contract.CodeNotFound, "not found")
}

func principal(role contract.Role) contract.Principal {
	return contract.Principal{UserID: "u", OrgID: "o", Role: role}
}

func TestAuthorizerLoadsGrantsOnlyWhenTheyMatter(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{grants: []access.Grant{{GroupID: "g1", WorkspaceID: wsA, ResourceType: "workspace", Action: "write"}}}
	a := &access.Authorizer{Repo: repo, Workspaces: fakeWS{wsA: "o", wsB: "o"}}

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
	a := &access.Authorizer{Repo: &fakeRepo{err: boom}, Workspaces: fakeWS{wsA: "o"}}
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

// A workspace of another org is not_found, exactly like an unknown one: the directory lookup is
// not org-scoped, so the Authorizer compares the org itself.
func TestAuthorizerTreatsAForeignOrgWorkspaceAsNotFound(t *testing.T) {
	a := &access.Authorizer{Repo: &fakeRepo{}, Workspaces: fakeWS{wsA: "other-org"}}
	for _, role := range []contract.Role{"admin", "member"} {
		if _, err := a.Authorize(context.Background(), principal(role), contract.PermWorkspaceRead, wsA); contract.AsError(err).Code != contract.CodeNotFound {
			t.Errorf("%s: %v", role, err)
		}
	}
}

// A Scope answers any number of Filter calls with at most one grant read, and none for an admin.
func TestScopeReadsGrantsOnceForAnyNumberOfFilters(t *testing.T) {
	ctx := context.Background()
	repo := &fakeRepo{grants: []access.Grant{{GroupID: "g1", WorkspaceID: wsA, ResourceType: "workspace", Action: "read"}}}
	a := &access.Authorizer{Repo: repo, Workspaces: fakeWS{wsA: "o", wsB: "o"}}
	ids := []string{wsA, wsB}

	sc := a.Scope(ctx, principal("member"))
	for range 3 {
		got, err := sc.Filter(contract.PermWorkspaceRead, ids)
		want, _ := a.Filter(ctx, principal("member"), contract.PermWorkspaceRead, ids)
		if err != nil || len(got) != 1 || got[0] != wsA || len(want) != 1 {
			t.Fatalf("read filter = %v, %v (want %v)", got, err, want)
		}
	}
	if got, err := sc.Filter(contract.PermWorkspaceURLRead, ids); err != nil || len(got) != 0 {
		t.Fatalf("url filter = %v, %v", got, err)
	}
	repo.calls = 0
	sc = a.Scope(ctx, principal("member"))
	_, _ = sc.Filter(contract.PermWorkspaceRead, ids)
	_, _ = sc.Filter(contract.PermWorkspaceURLRead, ids)
	if repo.calls != 1 {
		t.Fatalf("grant reads = %d, want 1", repo.calls)
	}

	repo.calls = 0
	adm := a.Scope(ctx, principal("admin"))
	if got, err := adm.Filter(contract.PermWorkspaceURLRead, ids); err != nil || len(got) != 2 || repo.calls != 0 {
		t.Fatalf("admin = %v, %v, reads %d", got, err, repo.calls)
	}

	repo.err = errors.New("db down")
	if _, err := a.Scope(ctx, principal("member")).Filter(contract.PermWorkspaceRead, ids); err == nil {
		t.Fatal("a failed grant read must be an error, never an empty list")
	}
}
