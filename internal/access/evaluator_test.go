package access_test

import (
	"reflect"
	"testing"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/contract"
)

const (
	wsA = "00000000-0000-0000-0000-00000000000a"
	wsB = "00000000-0000-0000-0000-00000000000b"
)

func req(role contract.Role, perm contract.Permission, ws string) access.Request {
	return access.Request{Subject: access.Subject{UserID: "u", Role: role}, Permission: perm, WorkspaceID: ws}
}

func TestEvaluate(t *testing.T) {
	group := func(id, ws, rt, act string) access.Grant {
		return access.Grant{GroupID: id, WorkspaceID: ws, ResourceType: rt, Action: act}
	}
	org := func(ws, rt, act string) access.Grant {
		return access.Grant{WorkspaceID: ws, ResourceType: rt, Action: act}
	}
	tests := []struct {
		name   string
		req    access.Request
		grants []access.Grant
		want   access.Result
	}{
		{"admin with zero grant rows does everything", req("admin", contract.PermWorkspaceManage, wsA), nil, access.Result{Allowed: true, Reason: "admin"}},
		{"admin on an org-scoped permission", req("admin", contract.PermMemberManage, ""), nil, access.Result{Allowed: true, Reason: "admin"}},
		{"admin passes PermAdmin", req("admin", contract.PermAdmin, ""), nil, access.Result{Allowed: true, Reason: "admin"}},
		{"admin ignores a workspace-less workspace permission (route table guards that)", req("admin", contract.PermWorkspaceRead, ""), nil, access.Result{Allowed: true, Reason: "admin"}},
		{"member with no grants is denied", req("member", contract.PermWorkspaceRead, wsA), nil, access.Result{Reason: "no matching grant"}},
		{"member allowed by a group grant on that workspace", req("member", contract.PermWorkspaceWrite, wsA), []access.Grant{group("g1", wsA, "workspace", "write")}, access.Result{Allowed: true, Reason: "group:g1"}},
		{"group grant on another workspace does not apply", req("member", contract.PermWorkspaceWrite, wsA), []access.Grant{group("g1", wsB, "workspace", "write")}, access.Result{Reason: "no matching grant"}},
		{"org default with null workspace applies to every workspace", req("member", contract.PermWorkspaceRead, wsB), []access.Grant{org("", "workspace", "read")}, access.Result{Allowed: true, Reason: "org-default"}},
		{"org default on one workspace applies to that workspace only", req("member", contract.PermWorkspaceRead, wsB), []access.Grant{org(wsA, "workspace", "read")}, access.Result{Reason: "no matching grant"}},
		{"org default on one workspace applies there", req("member", contract.PermWorkspaceRead, wsA), []access.Grant{org(wsA, "workspace", "read")}, access.Result{Allowed: true, Reason: "org-default"}},
		{"the union: a group wins the reason over an org default", req("member", contract.PermSavingsRead, wsA), []access.Grant{org("", "savings", "read"), group("g2", wsA, "savings", "read")}, access.Result{Allowed: true, Reason: "group:g2"}},
		{"the smallest group id names the reason", req("member", contract.PermSavingsRead, wsA), []access.Grant{group("g9", wsA, "savings", "read"), group("g3", "", "savings", "read")}, access.Result{Allowed: true, Reason: "group:g3"}},
		{"read does not imply write", req("member", contract.PermWorkspaceWrite, wsA), []access.Grant{group("g1", wsA, "workspace", "read")}, access.Result{Reason: "no matching grant"}},
		{"another resource type does not apply", req("member", contract.PermSessionRead, wsA), []access.Grant{group("g1", wsA, "savings", "read")}, access.Result{Reason: "no matching grant"}},
		{"org-scoped permission needs a null-workspace grant", req("member", contract.PermMemberManage, ""), []access.Grant{group("g1", "", "member", "manage")}, access.Result{Allowed: true, Reason: "group:g1"}},
		{"org-scoped permission is not conferred by a workspace-bound row", req("member", contract.PermMemberManage, ""), []access.Grant{group("g1", wsA, "member", "manage")}, access.Result{Reason: "no matching grant"}},
		{"a workspace-scoped permission with no workspace is denied", req("member", contract.PermWorkspaceRead, ""), []access.Grant{org("", "workspace", "read")}, access.Result{Reason: "no matching grant"}},
		{"member passes PermMember", req("member", contract.PermMember, ""), nil, access.Result{Allowed: true, Reason: "member"}},
		{"member fails PermAdmin whatever the grants", req("member", contract.PermAdmin, ""), []access.Grant{group("g1", "", "member", "manage")}, access.Result{Reason: "admin required"}},
		{"unknown permission is denied", req("member", "workspace:destroy", wsA), []access.Grant{group("g1", wsA, "workspace", "destroy")}, access.Result{Reason: "no matching grant"}},
		{"unknown role is denied", req("owner", contract.PermMember, ""), nil, access.Result{Reason: "no matching grant"}},
		{"self is open", req("", contract.PermSelf, ""), nil, access.Result{Allowed: true, Reason: "self"}},
	}
	var ev access.PermissionEvaluator
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ev.Evaluate(tc.req, tc.grants); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestNeedsGrantsOnlyForAMemberAskingForATaxonomyPermission(t *testing.T) {
	var ev access.PermissionEvaluator
	for _, tc := range []struct {
		r    access.Request
		want bool
	}{
		{req("member", contract.PermWorkspaceRead, wsA), true},
		{req("admin", contract.PermWorkspaceRead, wsA), false},
		{req("member", contract.PermMember, ""), false},
		{req("member", contract.PermAdmin, ""), false},
		{req("member", contract.PermSelf, ""), false},
		{req("owner", contract.PermWorkspaceRead, wsA), false},
	} {
		if got := ev.NeedsGrants(tc.r); got != tc.want {
			t.Errorf("%+v: %v", tc.r, got)
		}
	}
}

func TestSnapshotIsAFlattenedSortedUnion(t *testing.T) {
	var ev access.PermissionEvaluator
	if got := ev.Snapshot(access.Subject{Role: "admin"}, []access.Grant{{ResourceType: "workspace", Action: "read"}}); !got.Admin || got.Grants == nil || len(got.Grants) != 0 {
		t.Fatalf("admin snapshot: %+v", got)
	}
	got := ev.Snapshot(access.Subject{Role: "member"}, []access.Grant{
		{GroupID: "g1", WorkspaceID: wsB, ResourceType: "savings", Action: "read"},
		{ResourceType: "savings", Action: "read"},
		{GroupID: "g2", WorkspaceID: wsB, ResourceType: "savings", Action: "read"}, // duplicate of the first
		{GroupID: "g1", ResourceType: "member", Action: "read"},
		{ResourceType: "nonsense", Action: "read"}, // not in the taxonomy: dropped
	})
	ws := wsB
	want := []contract.Grant{
		{ResourceType: "member", Action: "read"},
		{ResourceType: "savings", Action: "read"},
		{WorkspaceID: &ws, ResourceType: "savings", Action: "read"},
	}
	if got.Admin || !reflect.DeepEqual(got.Grants, want) {
		t.Fatalf("got %+v", got)
	}
	if empty := ev.Snapshot(access.Subject{Role: "member"}, nil); empty.Grants == nil {
		t.Fatal("grants must be an empty list, never null")
	}
}

func TestMayAssignRole(t *testing.T) {
	for _, tc := range []struct {
		actor, target contract.Role
		ok            bool
	}{
		{"admin", "admin", true}, {"admin", "member", true}, {"member", "member", true}, {"member", "admin", false}, {"member", "", false},
		// An actor that is not a valid role is refused before the target is looked at.
		{"", "member", false}, {"owner", "member", false}, {"owner", "admin", false},
	} {
		err := access.MayAssignRole(tc.actor, tc.target)
		if (err == nil) != tc.ok {
			t.Errorf("%s assigning %q: %v", tc.actor, tc.target, err)
		}
		if err != nil {
			if e := contract.AsError(err); e.Code != contract.CodeForbidden || e.Reason != "admin required" {
				t.Errorf("%+v", e)
			}
		}
	}
	if !access.IsAdmin("admin") || access.IsAdmin("member") {
		t.Fatal("IsAdmin")
	}
}
