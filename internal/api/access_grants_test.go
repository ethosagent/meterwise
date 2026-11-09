package api_test

import (
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Plan 03 section 9: an admin with zero grant rows can do everything.
func TestAdminWithZeroGrantRowsDoesEverything(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	// wipe every grant: org defaults replaced by nothing, and no group exists
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(), 200)
	for _, table := range []string{"organization_permissions", "group_permissions", "resource_permissions", "group_users", "group_workspaces", "groups"} {
		if n := s.count(t, "SELECT count(*) FROM "+table); n != 0 {
			t.Fatalf("%s has %d rows", table, n)
		}
	}
	ws := s.newWorkspace(t, admin.tok, "Alpha")
	s.must(t, admin.tok, "PATCH", "/api/workspaces/"+ws, `{"name":"Alpha2"}`, 200)
	s.must(t, admin.tok, "POST", "/api/workspaces/"+ws+"/switch", "", 200)
	s.must(t, admin.tok, "GET", "/api/members", "", 200)
	s.must(t, admin.tok, "GET", "/api/invitations", "", 200)
	s.must(t, admin.tok, "GET", "/api/organization/permissions", "", 200)
	if list := decodeAs[contract.WorkspaceList](t, s.must(t, admin.tok, "GET", "/api/workspaces", "", 200)); len(list.Items) != 2 {
		t.Fatalf("an admin sees every workspace: %+v", list)
	}
	snap := decodeAs[contract.PermissionSnapshot](t, s.must(t, admin.tok, "GET", "/api/me/permissions", "", 200))
	if !snap.Admin || len(snap.Grants) != 0 {
		t.Fatalf("admin snapshot: %+v", snap)
	}
	s.must(t, admin.tok, "DELETE", "/api/workspaces/"+ws, "", 204)
}

// A member is allowed iff the union of group grants and org defaults matches; each source and the absence.
func TestMemberIsAllowedIffAGrantMatches(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	a, b := s.newWorkspace(t, admin.tok, "A"), s.newWorkspace(t, admin.tok, "B")

	// the seeded org defaults: read everything, write nothing
	snap := decodeAs[contract.PermissionSnapshot](t, s.must(t, m.tok, "GET", "/api/me/permissions", "", 200))
	if snap.Admin || !hasGrant(snap.Grants, "", "workspace", "read") || !hasGrant(snap.Grants, "", "session", "read") || hasGrant(snap.Grants, "", "workspace", "write") {
		t.Fatalf("seeded defaults: %+v", snap)
	}
	if list := decodeAs[contract.WorkspaceList](t, s.must(t, m.tok, "GET", "/api/workspaces", "", 200)); len(list.Items) != 3 {
		t.Fatalf("default read lets a member list all 3 workspaces: %+v", list)
	}
	code, body := s.call(t, "PATCH", "/api/workspaces/"+a, m.tok, `{"name":"A2"}`)
	forbidden(t, code, body, "no matching grant")

	// source 1: a group grant on workspace A only
	grp := s.newGroup(t, admin.tok, "Editors")
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/workspaces", `{"workspaceId":"`+a+`"}`, 200)
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/members", `{"userId":"`+m.id+`"}`, 200)
	s.must(t, admin.tok, "PUT", "/api/groups/"+grp+"/permissions", grantsJSON(g(a, "workspace", "read"), g(a, "workspace", "write")), 200)
	s.must(t, m.tok, "PATCH", "/api/workspaces/"+a, `{"name":"A2"}`, 200)
	code, body = s.call(t, "PATCH", "/api/workspaces/"+b, m.tok, `{"name":"B2"}`)
	forbidden(t, code, body, "no matching grant") // a grant on A does not reach B

	// source 2: an org default with a null workspace applies to every workspace
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(g("", "workspace", "read"), g("", "workspace", "write")), 200)
	s.must(t, m.tok, "PATCH", "/api/workspaces/"+b, `{"name":"B2"}`, 200)

	// an org default on one workspace applies to that workspace only
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(g(b, "workspace", "write")), 200)
	s.must(t, m.tok, "PATCH", "/api/workspaces/"+b, `{"name":"B3"}`, 200)
	_, defID := s.defaultWorkspace(t, admin.tok)
	code, body = s.call(t, "PATCH", "/api/workspaces/"+defID, m.tok, `{"name":"D"}`)
	forbidden(t, code, body, "no matching grant")

	// absence: no group, no default
	s.must(t, admin.tok, "DELETE", "/api/groups/"+grp, "", 204)
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(), 200)
	code, body = s.call(t, "PATCH", "/api/workspaces/"+a, m.tok, `{"name":"A3"}`)
	forbidden(t, code, body, "no matching grant")
	if list := decodeAs[contract.WorkspaceList](t, s.must(t, m.tok, "GET", "/api/workspaces", "", 200)); len(list.Items) != 0 {
		t.Fatalf("with no grant a member lists nothing: %+v", list)
	}
}

// delete needs workspace:manage; the escalation chain stops at the member's own grants.
func TestWorkspaceManageGateDeletion(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	a := s.newWorkspace(t, admin.tok, "A")
	grp := s.newGroup(t, admin.tok, "Ops")
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/workspaces", `{"workspaceId":"`+a+`"}`, 200)
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/members", `{"userId":"`+m.id+`"}`, 200)
	s.must(t, admin.tok, "PUT", "/api/groups/"+grp+"/permissions", grantsJSON(g(a, "workspace", "write")), 200)
	code, b := s.call(t, "DELETE", "/api/workspaces/"+a, m.tok, "")
	forbidden(t, code, b, "no matching grant") // write is not manage
	s.must(t, admin.tok, "PUT", "/api/groups/"+grp+"/permissions", grantsJSON(g(a, "workspace", "manage")), 200)
	s.must(t, m.tok, "DELETE", "/api/workspaces/"+a, "", 204)
	// the workspace is gone, so are its grants (cascade), and the group stays
	if n := s.count(t, "SELECT count(*) FROM group_workspaces"); n != 0 {
		t.Fatalf("group_workspaces rows left: %d", n)
	}
}

// The active workspace follows readability: stored one if readable, else the first readable (default
// first), else null.
func TestMeActiveWorkspaceFollowsReadAccess(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	_, defID := s.defaultWorkspace(t, admin.tok)
	a := s.newWorkspace(t, admin.tok, "A")
	if got := activeOf(s.meOf(t, m.tok)); got != defID {
		t.Fatalf("fresh member lands on the default: %q", got)
	}
	s.must(t, m.tok, "POST", "/api/workspaces/"+a+"/switch", "", 200)
	if got := activeOf(s.meOf(t, m.tok)); got != a {
		t.Fatalf("stored workspace: %q", got)
	}
	// lose read on A (keep the default): fall back to the default
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(g(defID, "workspace", "read")), 200)
	if got := activeOf(s.meOf(t, m.tok)); got != defID {
		t.Fatalf("fallback: %q", got)
	}
	// lose everything: null
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(), 200)
	me := s.meOf(t, m.tok)
	if me.ActiveWorkspaceID != nil {
		t.Fatalf("no readable workspace must be null: %q", *me.ActiveWorkspaceID)
	}
	code, b := s.call(t, "POST", "/api/workspaces/"+defID+"/switch", m.tok, "")
	forbidden(t, code, b, "no matching grant")
}

func TestUnknownResourceTypeAndActionAreRejectedAtTheBoundary(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	a := s.newWorkspace(t, admin.tok, "A")
	grp := s.newGroup(t, admin.tok, "G")
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/workspaces", `{"workspaceId":"`+a+`"}`, 200)
	before := grantList(t, s.must(t, admin.tok, "GET", "/api/groups/"+grp+"/permissions", "", 200))
	for name, body := range map[string]string{
		"unknown type":              `{"grants":[{"workspaceId":"` + a + `","resourceType":"nope","action":"read"}]}`,
		"unknown action":            `{"grants":[{"workspaceId":"` + a + `","resourceType":"workspace","action":"destroy"}]}`,
		"org scoped with workspace": `{"grants":[{"workspaceId":"` + a + `","resourceType":"member","action":"read"}]}`,
		"workspace scoped no ws":    `{"grants":[{"workspaceId":null,"resourceType":"workspace","action":"read"}]}`,
		"unattached workspace":      `{"grants":[{"workspaceId":"00000000-0000-0000-0000-00000000abcd","resourceType":"workspace","action":"read"}]}`,
		"good then bad":             `{"grants":[{"workspaceId":"` + a + `","resourceType":"workspace","action":"write"},{"workspaceId":null,"resourceType":"nope","action":"x"}]}`,
		"unknown field":             `{"grants":[],"extra":1}`,
	} {
		code, b := s.call(t, "PUT", "/api/groups/"+grp+"/permissions", admin.tok, body)
		if e := decodeErr(t, b); code != 422 || e.Code != contract.CodeValidationFailed {
			t.Errorf("%s: %d %s", name, code, b)
		}
	}
	after := grantList(t, s.must(t, admin.tok, "GET", "/api/groups/"+grp+"/permissions", "", 200))
	if len(after) != len(before) {
		t.Fatalf("a rejected PUT changed the grants: %v -> %v", before, after)
	}
	// org defaults: org-scoped with a workspace and unknown names
	for name, body := range map[string]string{
		"org scoped with workspace": `{"grants":[{"workspaceId":"` + a + `","resourceType":"audit_log","action":"read"}]}`,
		"unknown type":              `{"grants":[{"workspaceId":null,"resourceType":"nope","action":"read"}]}`,
		"unknown workspace":         `{"grants":[{"workspaceId":"00000000-0000-0000-0000-00000000abcd","resourceType":"workspace","action":"read"}]}`,
		"malformed workspace":       `{"grants":[{"workspaceId":"zzz","resourceType":"workspace","action":"read"}]}`,
	} {
		code, b := s.call(t, "PUT", "/api/organization/permissions", admin.tok, body)
		if e := decodeErr(t, b); code != 422 || e.Code != contract.CodeValidationFailed {
			t.Errorf("org %s: %d %s", name, code, b)
		}
	}
	if got := grantList(t, s.must(t, admin.tok, "GET", "/api/organization/permissions", "", 200)); len(got) != 5 {
		t.Fatalf("a rejected org PUT changed the defaults: %v", got)
	}
}
