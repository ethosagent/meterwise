package api_test

import (
	"context"
	"testing"

	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
)

// A claim carrying whitespace or non-ASCII anywhere in the address is never trimmed into a match:
// it cannot claim an invitation, over the real HTTP stack with signed tokens.
func TestWhitespaceInAnEmailClaimNeverClaimsAnInvitation(t *testing.T) {
	s := newStack(t)
	root := s.admin(t, "root")
	pads := []struct{ name, pad string }{
		{"no-break space U+00A0", "\u00a0"}, {"ideographic space U+3000", "\u3000"}, {"em space U+2003", "\u2003"},
		{"next line U+0085", "\u0085"}, {"line separator U+2028", "\u2028"}, {"ogham space U+1680", "\u1680"},
		{"tab", "\t"}, {"ascii space", " "}, {"newline", "\n"},
	}
	i := 0
	for _, p := range pads {
		for _, pos := range []string{"prefix", "suffix", "inside"} {
			i++
			addr := "kate" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "@corp.test" // one invitation per attempt
			s.must(t, root.tok, "POST", "/api/invitations", `{"email":"`+addr+`","role":"admin"}`, 201)
			claim := map[string]string{"prefix": p.pad + addr, "suffix": addr + p.pad, "inside": addr[:4] + p.pad + addr[4:]}[pos]
			tok := s.mock.Token(clerkmock.Claims{Subject: "impostor_" + addr, Email: claim})
			if code, b := s.call(t, "POST", "/api/auth/complete-signup", tok, ""); code != 403 {
				t.Errorf("%s %s: complete-signup %d %s, want 403", p.name, pos, code, b)
			}
			if n := s.count(t, `SELECT count(*) FROM invitations WHERE email = $1 AND accepted_at IS NULL AND revoked_at IS NULL`, addr); n != 1 {
				t.Errorf("%s %s: the invitation was claimed (%d open)", p.name, pos, n)
			}
		}
	}
	if n := s.count(t, `SELECT count(*) FROM memberships WHERE role = 'admin'`); n != 1 {
		t.Fatalf("%d admins, want only root", n)
	}
	// the exact address still works
	s.must(t, root.tok, "POST", "/api/invitations", `{"email":"kate@corp.test","role":"member"}`, 201)
	s.must(t, s.token("kate", "kate@corp.test"), "POST", "/api/auth/complete-signup", "", 200)
}

// users.email is refreshed only from a verified claim. A fired admin who presents an unverified
// claim for the root admin's address must not take that address over: when root is later deleted
// and the operator runs bootstrap-admin for root, the fired admin must not come back.
func TestAnUnverifiedEmailClaimCannotBeUsedToGetAdminBack(t *testing.T) {
	s := newStack(t)
	root := s.admin(t, "root")
	fired := s.invited(t, root.tok, "fired", "admin")
	s.must(t, root.tok, "DELETE", "/api/members/"+fired.id, "", 204)

	spoof := s.mock.Token(clerkmock.Claims{Subject: "fired", Email: root.email, Unverified: true})
	s.call(t, "GET", "/api/me/preferences", spoof, "")
	if n := s.count(t, `SELECT count(*) FROM users WHERE clerk_user_id = 'fired' AND email = $1`, root.email); n != 0 {
		t.Fatal("an unverified claim overwrote users.email")
	}

	if err := s.svc.DeleteWebhookUser(context.Background(), "root"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.svc.BootstrapAdmin(context.Background(), root.email); err != nil {
		t.Fatalf("recovery for root: %v", err)
	}
	if code, _ := s.call(t, "GET", "/api/members", fired.tok, ""); code != 403 {
		t.Fatalf("the fired admin is back (GET /api/members %d)", code)
	}
	if n := s.count(t, `SELECT count(*) FROM memberships WHERE status = 'active'`); n != 0 {
		t.Fatalf("%d active memberships, want none until root signs up again", n)
	}
	if n := s.count(t, `SELECT count(*) FROM invitations WHERE email = $1 AND role = 'admin' AND accepted_at IS NULL AND revoked_at IS NULL`, root.email); n != 1 {
		t.Fatal("root got no admin invitation")
	}
}
