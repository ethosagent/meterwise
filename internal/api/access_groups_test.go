package api_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestGroupCRUDAndNames(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	grp := decodeAs[contract.Group](t, s.must(t, admin.tok, "POST", "/api/groups", `{"name":"  Platform  "}`, 201))
	if grp.Name != "Platform" || grp.MemberCount != 0 || grp.WorkspaceIDs == nil || len(grp.WorkspaceIDs) != 0 || grp.CreatedAt == "" {
		t.Fatalf("%+v", grp)
	}
	for _, name := range []string{"Platform", "platform", " PLATFORM "} {
		code, b := s.call(t, "POST", "/api/groups", admin.tok, `{"name":"`+name+`"}`)
		wantErr(t, code, b, 409, contract.CodeGroupNameTaken)
	}
	for name, body := range map[string]string{"empty": `{"name":""}`, "blank": `{"name":"   "}`, "long": `{"name":"` + strings.Repeat("x", 81) + `"}`, "control": `{"name":"a\u0007b"}`, "unknown field": `{"name":"x","y":1}`} {
		code, b := s.call(t, "POST", "/api/groups", admin.tok, body)
		if e := decodeErr(t, b); code != 422 || e.Code != contract.CodeValidationFailed {
			t.Errorf("%s: %d %s", name, code, b)
		}
	}
	other := s.newGroup(t, admin.tok, "Other")
	code, b := s.call(t, "PATCH", "/api/groups/"+other, admin.tok, `{"name":"platform"}`)
	wantErr(t, code, b, 409, contract.CodeGroupNameTaken)
	renamed := decodeAs[contract.Group](t, s.must(t, admin.tok, "PATCH", "/api/groups/"+grp.ID, `{"name":"Platform Team"}`, 200))
	if renamed.Name != "Platform Team" {
		t.Fatalf("%+v", renamed)
	}
	// renaming to its own name (different case) is not a conflict with itself
	s.must(t, admin.tok, "PATCH", "/api/groups/"+grp.ID, `{"name":"platform team"}`, 200)
	list := decodeAs[contract.GroupList](t, s.must(t, admin.tok, "GET", "/api/groups", "", 200))
	if len(list.Items) != 2 || list.Items[0].Name != "Other" {
		t.Fatalf("list (by name): %+v", list)
	}
	s.must(t, admin.tok, "GET", "/api/groups/"+grp.ID, "", 200)
	for _, m := range []string{"GET", "PATCH", "DELETE"} {
		for _, id := range []string{"00000000-0000-0000-0000-000000000042", "nope"} {
			code, b := s.call(t, m, "/api/groups/"+id, admin.tok, `{"name":"x"}`)
			wantErr(t, code, b, 404, contract.CodeNotFound)
		}
	}
	for _, p := range []string{"/permissions", "/members"} {
		code, b := s.call(t, "GET", "/api/groups/00000000-0000-0000-0000-000000000042"+p, admin.tok, "")
		wantErr(t, code, b, 404, contract.CodeNotFound)
	}
	s.must(t, admin.tok, "DELETE", "/api/groups/"+grp.ID, "", 204)
	code, b = s.call(t, "DELETE", "/api/groups/"+grp.ID, admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
}

func TestMembersReadTheGroupsButOnlyAdminsWriteThem(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	grp := s.newGroup(t, admin.tok, "G")
	// the seeded defaults do not include member:read, so a plain member cannot see groups yet
	code, b := s.call(t, "GET", "/api/groups", m.tok, "")
	forbidden(t, code, b, "no matching grant")
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(g("", "member", "read")), 200)
	s.must(t, m.tok, "GET", "/api/groups", "", 200)
	s.must(t, m.tok, "GET", "/api/groups/"+grp, "", 200)
	s.must(t, m.tok, "GET", "/api/groups/"+grp+"/permissions", "", 200)
	s.must(t, m.tok, "GET", "/api/groups/"+grp+"/members", "", 200)
	s.must(t, m.tok, "GET", "/api/members", "", 200)
	s.must(t, m.tok, "GET", "/api/organization/permissions", "", 200)
	// member:manage does not unlock the admin-only writes
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(g("", "member", "read"), g("", "member", "manage")), 200)
	for _, r := range []struct{ method, path, body string }{
		{"POST", "/api/groups", `{"name":"x"}`},
		{"PATCH", "/api/groups/" + grp, `{"name":"x"}`},
		{"DELETE", "/api/groups/" + grp, ""},
		{"PUT", "/api/groups/" + grp + "/permissions", `{"grants":[]}`},
		{"POST", "/api/groups/" + grp + "/workspaces", `{"workspaceId":"00000000-0000-0000-0000-000000000042"}`},
		{"DELETE", "/api/groups/" + grp + "/workspaces/00000000-0000-0000-0000-000000000042", ""},
		{"POST", "/api/groups/" + grp + "/members", `{"userId":"` + m.id + `"}`},
		{"DELETE", "/api/groups/" + grp + "/members/" + m.id, ""},
		{"PUT", "/api/organization/permissions", `{"grants":[]}`},
		{"PATCH", "/api/members/" + m.id, `{"role":"admin"}`},
		{"DELETE", "/api/members/" + m.id, ""},
	} {
		code, b := s.call(t, r.method, r.path, m.tok, r.body)
		forbidden(t, code, b, "admin required")
	}
}

// Attaching seeds the read grants in one transaction; adding a member touches group_users only.
func TestAttachSeedsGrantsAndAddingAMemberTouchesOnlyGroupUsers(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	a := s.newWorkspace(t, admin.tok, "A")
	grp := s.newGroup(t, admin.tok, "G")

	grp1 := decodeAs[contract.Group](t, s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/workspaces", `{"workspaceId":"`+a+`"}`, 200))
	if len(grp1.WorkspaceIDs) != 1 || grp1.WorkspaceIDs[0] != a {
		t.Fatalf("%+v", grp1)
	}
	got := grantList(t, s.must(t, admin.tok, "GET", "/api/groups/"+grp+"/permissions", "", 200))
	for _, rt := range []string{"workspace", "workspace_url", "session", "monitor_config", "savings"} {
		if !hasGrant(got, a, rt, "read") {
			t.Errorf("attach did not seed %s:read on the workspace: %v", rt, got)
		}
	}
	if len(got) != 5 || s.count(t, "SELECT count(*) FROM group_permissions") != 5 || s.count(t, "SELECT count(*) FROM resource_permissions") != 5 {
		t.Fatalf("seed size: %v", got)
	}
	// idempotent
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/workspaces", `{"workspaceId":"`+a+`"}`, 200)
	if s.count(t, "SELECT count(*) FROM group_permissions") != 5 || s.count(t, "SELECT count(*) FROM group_workspaces") != 1 {
		t.Fatal("attaching twice duplicated rows")
	}
	// a workspace that is not there: 404 and nothing half-written
	code, b := s.call(t, "POST", "/api/groups/"+grp+"/workspaces", admin.tok, `{"workspaceId":"00000000-0000-0000-0000-000000000042"}`)
	wantErr(t, code, b, 404, contract.CodeNotFound)
	if s.count(t, "SELECT count(*) FROM group_workspaces") != 1 {
		t.Fatal("a failed attach left a group_workspaces row")
	}

	tables := []string{"group_permissions", "resource_permissions", "organization_permissions", "group_workspaces", "groups", "memberships"}
	before := map[string]int{}
	for _, tb := range tables {
		before[tb] = s.count(t, "SELECT count(*) FROM "+tb)
	}
	if s.count(t, "SELECT count(*) FROM group_users") != 0 {
		t.Fatal("setup")
	}
	list := decodeAs[contract.MemberList](t, s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/members", `{"userId":"`+m.id+`"}`, 200))
	if len(list.Items) != 1 || list.Items[0].UserID != m.id || len(list.Items[0].GroupIDs) != 1 {
		t.Fatalf("%+v", list)
	}
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/members", `{"userId":"`+m.id+`"}`, 200) // idempotent
	for _, tb := range tables {
		if now := s.count(t, "SELECT count(*) FROM "+tb); now != before[tb] {
			t.Errorf("adding a member changed %s: %d -> %d", tb, before[tb], now)
		}
	}
	if s.count(t, "SELECT count(*) FROM group_users") != 1 {
		t.Fatal("group_users")
	}
	if g1 := decodeAs[contract.Group](t, s.must(t, admin.tok, "GET", "/api/groups/"+grp, "", 200)); g1.MemberCount != 1 {
		t.Fatalf("%+v", g1)
	}
	// 422 for someone who is not an active member, 404 removing someone not in the group
	code, b = s.call(t, "POST", "/api/groups/"+grp+"/members", admin.tok, `{"userId":"00000000-0000-0000-0000-000000000042"}`)
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	code, b = s.call(t, "POST", "/api/groups/"+grp+"/members", admin.tok, `{"userId":"zzz"}`)
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	code, b = s.call(t, "DELETE", "/api/groups/"+grp+"/members/"+admin.id, admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
	s.must(t, admin.tok, "DELETE", "/api/groups/"+grp+"/members/"+m.id, "", 204)

	// detaching removes the row and every grant there, in one go; other workspaces are untouched
	b2 := s.newWorkspace(t, admin.tok, "B")
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/workspaces", `{"workspaceId":"`+b2+`"}`, 200)
	s.must(t, admin.tok, "DELETE", "/api/groups/"+grp+"/workspaces/"+a, "", 204)
	got = grantList(t, s.must(t, admin.tok, "GET", "/api/groups/"+grp+"/permissions", "", 200))
	if len(got) != 5 || hasGrant(got, a, "workspace", "read") || !hasGrant(got, b2, "workspace", "read") {
		t.Fatalf("after detach: %v", got)
	}
	code, b = s.call(t, "DELETE", "/api/groups/"+grp+"/workspaces/"+a, admin.tok, "")
	wantErr(t, code, b, 404, contract.CodeNotFound)
}

// Deleting a group takes its grants and memberships with it (a member loses what it gave).
func TestDeletingAGroupRevokesWhatItGranted(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	a := s.newWorkspace(t, admin.tok, "A")
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(), 200)
	grp := s.newGroup(t, admin.tok, "G")
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/workspaces", `{"workspaceId":"`+a+`"}`, 200)
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/members", `{"userId":"`+m.id+`"}`, 200)
	s.must(t, m.tok, "POST", "/api/workspaces/"+a+"/switch", "", 200)
	s.must(t, admin.tok, "DELETE", "/api/groups/"+grp, "", 204)
	code, b := s.call(t, "POST", "/api/workspaces/"+a+"/switch", m.tok, "")
	forbidden(t, code, b, "no matching grant")
	if s.count(t, "SELECT count(*) FROM group_permissions")+s.count(t, "SELECT count(*) FROM group_users")+s.count(t, "SELECT count(*) FROM group_workspaces") != 0 {
		t.Fatal("rows of a deleted group are still there")
	}
}

// PUT /api/groups/{id}/members replaces the member set in one transaction (Plan 03 carry B12).
func TestPutGroupMembersReplacesTheSetAtomically(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m1 := s.invited(t, admin.tok, "user_m1", contract.RoleMember)
	m2 := s.invited(t, admin.tok, "user_m2", contract.RoleMember)
	grp := s.newGroup(t, admin.tok, "G")
	put := func(tok, body string, want int) []byte {
		return s.must(t, tok, "PUT", "/api/groups/"+grp+"/members", body, want)
	}
	items := func(b []byte) []contract.Member { return decodeAs[contract.MemberList](t, b).Items }
	if got := items(put(admin.tok, `{"userIds":["`+m1.id+`","`+m2.id+`"]}`, 200)); len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	if got := items(put(admin.tok, `{"userIds":["`+m2.id+`"]}`, 200)); len(got) != 1 || got[0].UserID != m2.id {
		t.Fatalf("%+v", got)
	}
	code, b := s.call(t, "PUT", "/api/groups/"+grp+"/members", admin.tok, `{"userIds":["`+m1.id+`","00000000-0000-0000-0000-000000000042"]}`)
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
	if got := items(s.must(t, admin.tok, "GET", "/api/groups/"+grp+"/members", "", 200)); len(got) != 1 || got[0].UserID != m2.id {
		t.Fatalf("a refused PUT changed the set: %+v", got)
	}
	code, b = s.call(t, "PUT", "/api/groups/00000000-0000-0000-0000-000000000042/members", admin.tok, `{"userIds":[]}`)
	wantErr(t, code, b, 404, contract.CodeNotFound)
	code, b = s.call(t, "PUT", "/api/groups/"+grp+"/members", m1.tok, `{"userIds":[]}`)
	forbidden(t, code, b, "admin required")
	if got := items(put(admin.tok, `{"userIds":[]}`, 200)); got == nil || len(got) != 0 {
		t.Fatalf("clear: %#v", got)
	}
	code, b = s.call(t, "PUT", "/api/groups/"+grp+"/members", admin.tok, `{"userIds":"x"}`)
	wantErr(t, code, b, 422, contract.CodeValidationFailed)
}
