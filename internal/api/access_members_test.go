package api_test

import (
	"sync"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
)

func TestMemberListAndRoleChange(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	list := decodeAs[contract.MemberList](t, s.must(t, admin.tok, "GET", "/api/members", "", 200))
	if len(list.Items) != 2 {
		t.Fatalf("%+v", list)
	}
	got := decodeAs[contract.Member](t, s.must(t, admin.tok, "PATCH", "/api/members/"+m.id, `{"role":"admin"}`, 200))
	if got.Role != contract.RoleAdmin || got.UserID != m.id {
		t.Fatalf("%+v", got)
	}
	if r := s.meOf(t, m.tok).Role; r != contract.RoleAdmin {
		t.Fatalf("role not effective on the next request: %s", r)
	}
	s.must(t, admin.tok, "PATCH", "/api/members/"+m.id, `{"role":"member"}`, 200)
	for _, body := range []string{`{"role":"owner"}`, `{"role":""}`, `{}`, `{"role":"admin","x":1}`} {
		code, b := s.call(t, "PATCH", "/api/members/"+m.id, admin.tok, body)
		wantErr(t, code, b, 422, contract.CodeValidationFailed)
	}
	for _, id := range []string{"00000000-0000-0000-0000-000000000042", "nope"} {
		code, b := s.call(t, "PATCH", "/api/members/"+id, admin.tok, `{"role":"admin"}`)
		wantErr(t, code, b, 404, contract.CodeNotFound)
		code, b = s.call(t, "DELETE", "/api/members/"+id, admin.tok, "")
		wantErr(t, code, b, 404, contract.CodeNotFound)
	}
}

func TestLastAdminCannotBeDemotedOrRemoved(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	code, b := s.call(t, "PATCH", "/api/members/"+admin.id, admin.tok, `{"role":"member"}`)
	wantErr(t, code, b, 409, contract.CodeLastAdmin)
	code, b = s.call(t, "DELETE", "/api/members/"+admin.id, admin.tok, "")
	wantErr(t, code, b, 409, contract.CodeLastAdmin)
	if s.meOf(t, admin.tok).Role != contract.RoleAdmin {
		t.Fatal("the last admin lost the role")
	}
	// with a second admin the first may step down
	b2 := s.invited(t, admin.tok, "user_b", contract.RoleAdmin)
	s.must(t, admin.tok, "PATCH", "/api/members/"+admin.id, `{"role":"member"}`, 200)
	code, b = s.call(t, "DELETE", "/api/members/"+b2.id, b2.tok, "")
	wantErr(t, code, b, 409, contract.CodeLastAdmin)
}

func TestRemovingAMemberLocksThemOutAndRevokesTheirInvitations(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	grp := s.newGroup(t, admin.tok, "G")
	s.must(t, admin.tok, "POST", "/api/groups/"+grp+"/members", `{"userId":"`+m.id+`"}`, 200)
	s.must(t, admin.tok, "DELETE", "/api/members/"+m.id, "", 204)

	code, b := s.call(t, "GET", "/api/me", m.tok, "")
	wantErr(t, code, b, 403, contract.CodeNotInvited)
	if n := s.count(t, "SELECT count(*) FROM group_users"); n != 0 {
		t.Fatalf("group_users after removal: %d", n)
	}
	if n := s.count(t, "SELECT count(*) FROM invitations WHERE email=$1 AND accepted_at IS NULL AND revoked_at IS NULL", m.email); n != 0 {
		t.Fatalf("open invitations for the removed email: %d", n)
	}
	list := decodeAs[contract.MemberList](t, s.must(t, admin.tok, "GET", "/api/members", "", 200))
	if len(list.Items) != 1 {
		t.Fatalf("removed member still listed: %+v", list)
	}

	// re-invite and accept: active again with the invited role and no groups
	again := s.invited(t, admin.tok, "user_m", contract.RoleAdmin)
	if again.id != m.id {
		t.Fatalf("user id changed: %s vs %s", again.id, m.id)
	}
	if r := s.meOf(t, again.tok).Role; r != contract.RoleAdmin {
		t.Fatalf("role after re-invite: %s", r)
	}
	if n := s.count(t, "SELECT count(*) FROM group_users"); n != 0 {
		t.Fatalf("a returning member kept groups: %d", n)
	}
}

func TestRemovalRevokesAnOpenInvitationForThatEmail(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	// an open invitation for an active member is refused, so plant one the way a stale one arises
	if _, err := s.db.Pool().Exec(t.Context(), `INSERT INTO invitations (org_id, email, role, expires_at)
		SELECT ms.org_id, u.email, 'admin', now() + interval '1 day' FROM users u JOIN memberships ms ON ms.user_id=u.id WHERE u.id=$1`, m.id); err != nil {
		t.Fatal(err)
	}
	s.must(t, admin.tok, "DELETE", "/api/members/"+m.id, "", 204)
	if n := s.count(t, "SELECT count(*) FROM invitations WHERE email=$1 AND accepted_at IS NULL AND revoked_at IS NULL", m.email); n != 0 {
		t.Fatalf("stale invitation survived the removal: %d", n)
	}
	code, b := s.call(t, "GET", "/api/me", m.tok, "")
	wantErr(t, code, b, 403, contract.CodeNotInvited)
}

func TestMemberManageGrantsInvitingMembersNotAdmins(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	s.must(t, admin.tok, "PUT", "/api/organization/permissions", grantsJSON(g("", "member", "manage")), 200)

	s.must(t, m.tok, "POST", "/api/invitations", `{"email":"new@example.test","role":"member"}`, 201)
	code, b := s.call(t, "POST", "/api/invitations", m.tok, `{"email":"boss@example.test","role":"admin"}`)
	forbidden(t, code, b, "admin required")

	adm := decodeAs[contract.InvitationView](t, s.must(t, admin.tok, "POST", "/api/invitations", `{"email":"boss2@example.test","role":"admin"}`, 201))
	code, b = s.call(t, "POST", "/api/invitations/"+adm.ID+"/resend", m.tok, "")
	forbidden(t, code, b, "admin required")
	code, b = s.call(t, "DELETE", "/api/invitations/"+adm.ID, m.tok, "")
	forbidden(t, code, b, "admin required")
	s.must(t, admin.tok, "DELETE", "/api/invitations/"+adm.ID, "", 204)

	// inviting someone who is already an active member is a conflict
	code, b = s.call(t, "POST", "/api/invitations", admin.tok, `{"email":"`+m.email+`","role":"member"}`)
	wantErr(t, code, b, 409, contract.CodeAlreadyMember)
}

func TestMePermissions(t *testing.T) {
	s := newStack(t)
	admin := s.admin(t, "user_admin")
	m := s.invited(t, admin.tok, "user_m", contract.RoleMember)
	snap := decodeAs[contract.PermissionSnapshot](t, s.must(t, admin.tok, "GET", "/api/me/permissions", "", 200))
	if !snap.Admin {
		t.Fatalf("admin: %+v", snap)
	}
	snap = decodeAs[contract.PermissionSnapshot](t, s.must(t, m.tok, "GET", "/api/me/permissions", "", 200))
	if snap.Admin || len(snap.Grants) == 0 {
		t.Fatalf("member: %+v", snap)
	}
	stranger := s.token("user_x", "x@example.test")
	code, b := s.call(t, "GET", "/api/me/permissions", stranger, "")
	wantErr(t, code, b, 403, contract.CodeNotInvited)
	code, b = s.call(t, "GET", "/api/me/permissions", "", "")
	wantErr(t, code, b, 401, contract.CodeUnauthenticated)
}

// Two admins demote each other at the same moment; the advisory lock plus the in-transaction
// count must leave exactly one admin, every round.
func TestConcurrentDemotionLeavesExactlyOneAdmin(t *testing.T) {
	s := newStack(t)
	a := s.admin(t, "user_a")
	b := s.invited(t, a.tok, "user_b", contract.RoleAdmin)
	for round := 0; round < 8; round++ {
		var wg sync.WaitGroup
		start := make(chan struct{})
		codes := make([]int, 2)
		for i, req := range []struct {
			actor who
			os    who
		}{{a, b}, {b, a}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes[i], _ = s.call(t, "PATCH", "/api/members/"+req.os.id, req.actor.tok, `{"role":"member"}`)
			}()
		}
		close(start)
		wg.Wait()
		if n := s.count(t, "SELECT count(*) FROM memberships WHERE role='admin' AND status='active'"); n != 1 {
			t.Fatalf("round %d: %d admins left (codes %v)", round, n, codes)
		}
		ok, conflict := 0, 0
		for _, c := range codes {
			switch c {
			case 200:
				ok++
			case 409, 403:
				conflict++
			}
		}
		if ok < 1 || ok+conflict != 2 {
			t.Fatalf("round %d: unexpected codes %v", round, codes)
		}
		// restore two admins; the demoted one may be locked out of nothing (still a member)
		s.must(t, adminOf(t, s, a, b), "PATCH", "/api/members/"+demotedOf(t, s, a, b)+"", `{"role":"admin"}`, 200)
	}
}

func TestConcurrentRemovalLeavesExactlyOneAdmin(t *testing.T) {
	s := newStack(t)
	a := s.admin(t, "user_a")
	b := s.invited(t, a.tok, "user_b", contract.RoleAdmin)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, p := range [][2]who{{a, b}, {b, a}} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			s.call(t, "DELETE", "/api/members/"+p[1].id, p[0].tok, "")
		}()
	}
	close(start)
	wg.Wait()
	if n := s.count(t, "SELECT count(*) FROM memberships WHERE role='admin' AND status='active'"); n != 1 {
		t.Fatalf("%d active admins", n)
	}
}

// adminOf and demotedOf find which of two people is the remaining admin after a demotion race.
func adminOf(t *testing.T, s *stack, a, b who) string {
	t.Helper()
	if s.meOf2(t, a.tok) == contract.RoleAdmin {
		return a.tok
	}
	return b.tok
}

func demotedOf(t *testing.T, s *stack, a, b who) string {
	t.Helper()
	if s.meOf2(t, a.tok) == contract.RoleAdmin {
		return b.id
	}
	return a.id
}

func (s *stack) meOf2(t *testing.T, tok string) contract.Role { return s.meOf(t, tok).Role }
