package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
	"github.com/ethosagent/meterwise/internal/store"
)

func TestInvitedUserFirstLoginCreatesMembershipAndLandsInTheDefaultWorkspace(t *testing.T) {
	s := newStack(t)
	// the CLI creates the first admin invitation
	inv, created, err := s.svc.BootstrapAdmin(context.Background(), "Admin@Example.test")
	if err != nil || !created || inv.Email != "admin@example.test" || inv.Role != contract.RoleAdmin {
		t.Fatalf("bootstrap: %+v %v %v", inv, created, err)
	}
	if got := s.mock.Invitations(); len(got) != 1 || got[0].Email != "admin@example.test" {
		t.Fatalf("Clerk was asked for %+v", got)
	}
	tok := s.token("user_admin", "admin@example.test")

	// zero webhooks so far: the first request mirrors the user lazily, and is not a member yet
	code, b := s.call(t, "GET", "/api/me", tok, "")
	wantErr(t, code, b, 403, contract.CodeNotInvited)

	code, b = s.call(t, "POST", "/api/auth/complete-signup", tok, "")
	if code != 200 {
		t.Fatalf("complete-signup: %d %s", code, b)
	}
	me := decodeAs[contract.Me](t, b)
	_, defaults := s.defaultWorkspace(t, tok)
	if me.Role != contract.RoleAdmin || me.OrgID == "" || me.User.Email != "admin@example.test" || activeOf(me) != defaults {
		t.Fatalf("me = %+v, want admin landing in default workspace %s", me, defaults)
	}
	if code, b = s.call(t, "GET", "/api/me", tok, ""); code != 200 {
		t.Fatalf("me after signup: %d %s", code, b)
	}
	// the invitation is consumed
	code, b = s.call(t, "GET", "/api/invitations", tok, "")
	list := decodeAs[contract.InvitationList](t, b)
	if code != 200 || len(list.Items) != 1 || list.Items[0].Status != contract.InvitationAccepted {
		t.Fatalf("invitations after signup: %d %s", code, b)
	}
	// complete-signup again is idempotent
	if code, _ = s.call(t, "POST", "/api/auth/complete-signup", tok, ""); code != 200 {
		t.Fatalf("second complete-signup: %d", code)
	}
}

func (s *stack) defaultWorkspace(t *testing.T, tok string) (publicID, id string) {
	t.Helper()
	code, b := s.call(t, "GET", "/api/workspaces", tok, "")
	if code != 200 {
		t.Fatalf("list workspaces: %d %s", code, b)
	}
	for _, w := range decodeAs[contract.WorkspaceList](t, b).Items {
		if w.IsDefault {
			return w.PublicID, w.ID
		}
	}
	t.Fatal("no default workspace")
	return "", ""
}

func TestUninvitedUserGetsNotInvitedEverywhere(t *testing.T) {
	s := newStack(t)
	tok := s.token("user_stranger", "stranger@example.test")
	for _, r := range [][2]string{{"GET", "/api/me"}, {"POST", "/api/auth/complete-signup"}, {"GET", "/api/workspaces"}, {"POST", "/api/workspaces"}, {"GET", "/api/invitations"}} {
		code, b := s.call(t, r[0], r[1], tok, `{"name":"x"}`)
		wantErr(t, code, b, 403, contract.CodeNotInvited)
	}
}

func TestEveryBadTokenIsUnauthenticated(t *testing.T) {
	s := newStack(t)
	ok := func(c clerkmock.Claims) string {
		c.Subject, c.Email = "user_x", "x@example.test"
		return s.mock.Token(c)
	}
	tests := []struct {
		name string
		edit func(*http.Request)
	}{
		{"no header", nil},
		{"wrong signature", bearer(ok(clerkmock.Claims{SignWith: clerkmock.ForeignKey()}))},
		{"wrong issuer", bearer(ok(clerkmock.Claims{Issuer: "https://clerk.evil.example"}))},
		{"expired", bearer(ok(clerkmock.Claims{ExpiresIn: -time.Hour}))},
		{"alg none", bearer(ok(clerkmock.Claims{Alg: "none"}))},
		{"unknown kid", bearer(ok(clerkmock.Claims{KeyID: "ins_unknown"}))},
		{"tampered payload", bearer(ok(clerkmock.Claims{TamperPayload: true}))},
		{"not a bearer scheme", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+ok(clerkmock.Claims{})) }},
		{"token only in a cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "__session", Value: ok(clerkmock.Claims{})}) }},
		{"empty bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") }},
	}
	for _, tc := range tests {
		for _, route := range []string{"/api/me", "/api/workspaces", "/api/me/preferences"} {
			t.Run(tc.name+" "+route, func(t *testing.T) {
				code, b := s.callWith(t, "GET", route, "", tc.edit)
				wantErr(t, code, b, 401, contract.CodeUnauthenticated)
				if strings.Contains(string(b), "Bearer") {
					t.Fatalf("body echoes credentials: %s", b)
				}
			})
		}
	}
	// the control token proves the harness accepts a good one (as a not-yet-invited user)
	code, b := s.call(t, "GET", "/api/me", ok(clerkmock.Claims{}), "")
	wantErr(t, code, b, 403, contract.CodeNotInvited)
}

func bearer(tok string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func TestInvitationMatchIsCaseNormalisedAndNeedsAVerifiedEmail(t *testing.T) {
	s := newStack(t)
	admin := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	code, b := s.call(t, "POST", "/api/invitations", admin, `{"email":"  Mixed.Case@Example.TEST ","role":"member"}`)
	if code != 201 || decodeAs[contract.InvitationView](t, b).Email != "mixed.case@example.test" {
		t.Fatalf("create: %d %s", code, b)
	}
	signup := func(c clerkmock.Claims) (int, []byte) {
		return s.call(t, "POST", "/api/auth/complete-signup", s.mock.Token(c), "")
	}
	// unverified email: refused, and the invitation stays pending
	code, b = signup(clerkmock.Claims{Subject: "user_m", Email: "mixed.case@example.test", Unverified: true})
	wantErr(t, code, b, 403, contract.CodeNotInvited)
	// verified but a different address
	code, b = signup(clerkmock.Claims{Subject: "user_m", Email: "mixed.case+other@example.test"})
	wantErr(t, code, b, 403, contract.CodeNotInvited)
	_, lb := s.call(t, "GET", "/api/invitations", admin, "")
	for _, i := range decodeAs[contract.InvitationList](t, lb).Items {
		if i.Email == "mixed.case@example.test" && i.Status != contract.InvitationPending {
			t.Fatalf("a refused signup consumed the invitation: %+v", i)
		}
	}
	// verified, different case: accepted with the invited role
	code, b = signup(clerkmock.Claims{Subject: "user_m", Email: "MIXED.case@EXAMPLE.test"})
	if code != 200 || decodeAs[contract.Me](t, b).Role != contract.RoleMember {
		t.Fatalf("matching signup: %d %s", code, b)
	}
}

func TestRemovedMembershipLosesAccessImmediately(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	if code, b := s.call(t, "GET", "/api/workspaces", tok, ""); code != 200 {
		t.Fatalf("before removal: %d %s", code, b)
	}
	me := func() contract.Me { _, b := s.call(t, "GET", "/api/me", tok, ""); return decodeAs[contract.Me](t, b) }
	userID := me().User.ID
	if err := s.db.SetMembershipStatus(context.Background(), store.OrgID(), userID, "removed"); err != nil {
		t.Fatal(err)
	}
	// same token, next request
	for _, r := range [][2]string{{"GET", "/api/workspaces"}, {"GET", "/api/me"}, {"GET", "/api/invitations"}} {
		code, b := s.call(t, r[0], r[1], tok, "")
		wantErr(t, code, b, 403, contract.CodeNotInvited)
	}
}

func TestMemberCannotUseAdminRoutesButAdminCan(t *testing.T) {
	s := newStack(t)
	admin := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	member := s.signInAs(t, "user_member", "member@example.test", contract.RoleMember)
	_, b := s.call(t, "POST", "/api/invitations", admin, `{"email":"new@example.test","role":"member"}`)
	invID := decodeAs[contract.InvitationView](t, b).ID
	adminRoutes := [][3]string{
		{"GET", "/api/invitations", ""},
		{"POST", "/api/invitations", `{"email":"other@example.test","role":"member"}`},
		{"POST", "/api/invitations/" + invID + "/resend", ""},
		{"DELETE", "/api/invitations/" + invID, ""},
	}
	for _, r := range adminRoutes {
		code, b := s.call(t, r[0], r[1], member, r[2])
		wantErr(t, code, b, 403, contract.CodeForbidden)
	}
	// a member still has the member routes
	if code, b := s.call(t, "GET", "/api/workspaces", member, ""); code != 200 {
		t.Fatalf("member list workspaces: %d %s", code, b)
	}
	if code, b := s.call(t, "GET", "/api/invitations", admin, ""); code != 200 {
		t.Fatalf("admin list invitations: %d %s", code, b)
	}
	// nothing the member sent changed state
	_, lb := s.call(t, "GET", "/api/invitations", admin, "")
	for _, i := range decodeAs[contract.InvitationList](t, lb).Items {
		if i.Email == "other@example.test" || (i.ID == invID && i.Status != contract.InvitationPending) {
			t.Fatalf("a forbidden call took effect: %+v", i)
		}
	}
}

// Ruling: create is open to members; rename and delete are admin-only (Plan 03 replaces this with grants).
func TestMembersCreateWorkspacesButCannotRenameOrDeleteThem(t *testing.T) {
	s := newStack(t)
	admin := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	member := s.signInAs(t, "user_member", "member@example.test", contract.RoleMember)

	code, b := s.call(t, "POST", "/api/workspaces", member, `{"name":"Member made"}`)
	if code != 201 {
		t.Fatalf("member create: %d %s", code, b)
	}
	ws := decodeAs[contract.WorkspaceView](t, b)
	if code, b := s.call(t, "POST", "/api/workspaces/"+ws.ID+"/switch", member, ""); code != 200 {
		t.Fatalf("member switch: %d %s", code, b)
	}
	code, b = s.call(t, "PATCH", "/api/workspaces/"+ws.ID, member, `{"name":"Renamed by member"}`)
	wantErr(t, code, b, 403, contract.CodeForbidden)
	code, b = s.call(t, "DELETE", "/api/workspaces/"+ws.ID, member, "")
	wantErr(t, code, b, 403, contract.CodeForbidden)

	// the forbidden calls changed nothing: the workspace still exists under its name
	_, lb := s.call(t, "GET", "/api/workspaces", admin, "")
	found := false
	for _, w := range decodeAs[contract.WorkspaceList](t, lb).Items {
		if w.ID == ws.ID {
			found = true
			if w.Name != "Member made" {
				t.Fatalf("a forbidden rename took effect: %q", w.Name)
			}
		}
	}
	if !found {
		t.Fatal("a forbidden delete took effect")
	}
	// the admin still can
	if code, b := s.call(t, "PATCH", "/api/workspaces/"+ws.ID, admin, `{"name":"Renamed"}`); code != 200 {
		t.Fatalf("admin rename: %d %s", code, b)
	}
	if code, b := s.call(t, "DELETE", "/api/workspaces/"+ws.ID, admin, ""); code != 204 {
		t.Fatalf("admin delete: %d %s", code, b)
	}
}

func TestPreferencesAndMeShareTheStoredPreferences(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	if code, b := s.call(t, "PATCH", "/api/me/preferences", tok, `{"theme":"dark","reducedMotion":true}`); code != 200 {
		t.Fatalf("patch: %d %s", code, b)
	}
	_, b := s.call(t, "GET", "/api/me", tok, "")
	if me := decodeAs[contract.Me](t, b); me.Preferences.Theme != contract.ThemeDark || !me.Preferences.ReducedMotion {
		t.Fatalf("me.preferences = %+v", me.Preferences)
	}
	// a fresh process sees them: they live in the database, not in memory
	_, b = s.call(t, "GET", "/api/me/preferences", tok, "")
	if strings.TrimSpace(string(b)) != `{"theme":"dark","reducedMotion":true}` {
		t.Fatalf("persisted: %s", b)
	}
}

func TestPublicRoutesNeedNoToken(t *testing.T) {
	s := newStack(t)
	if code, _ := s.call(t, "GET", "/api/config", "", ""); code != 200 {
		t.Fatalf("config: %d", code)
	}
	if code, _ := s.call(t, "GET", "/healthz", "", ""); code != 200 {
		t.Fatalf("healthz: %d", code)
	}
}
