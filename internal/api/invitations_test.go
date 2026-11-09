package api_test

import (
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
)

func TestInvitationLifecycle(t *testing.T) {
	s := newStack(t)
	admin := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)

	code, b := s.call(t, "POST", "/api/invitations", admin, `{"email":"Dev@Example.test","role":"member"}`)
	inv := decodeAs[contract.InvitationView](t, b)
	if code != 201 || inv.Email != "dev@example.test" || inv.Role != contract.RoleMember || inv.Status != contract.InvitationPending ||
		inv.InvitedBy != "admin@example.test" || inv.ID == "" {
		t.Fatalf("create: %d %s", code, b)
	}
	if exp, err := time.Parse(time.RFC3339, inv.ExpiresAt); err != nil || time.Until(exp) < 6*24*time.Hour {
		t.Fatalf("expiresAt %q is not about 7 days out", inv.ExpiresAt)
	}
	invites := s.mock.Invitations()
	if len(invites) != 2 || invites[1].Email != "dev@example.test" { // [0] is the admin's own bootstrap invitation
		t.Fatalf("Clerk saw %+v", invites)
	}

	// the same email again, in any case: 409
	for _, email := range []string{"dev@example.test", "DEV@EXAMPLE.TEST"} {
		code, b = s.call(t, "POST", "/api/invitations", admin, `{"email":"`+email+`","role":"admin"}`)
		wantErr(t, code, b, 409, contract.CodeDuplicateInvitation)
	}

	// resend: same row, a fresh Clerk invitation, the old one revoked
	code, b = s.call(t, "POST", "/api/invitations/"+inv.ID+"/resend", admin, "")
	if code != 200 || decodeAs[contract.InvitationView](t, b).ID != inv.ID {
		t.Fatalf("resend: %d %s", code, b)
	}
	invites = s.mock.Invitations()
	if len(invites) != 3 || !invites[1].Revoked || invites[2].Revoked || invites[2].Email != "dev@example.test" {
		t.Fatalf("after resend Clerk saw %+v", invites)
	}

	// revoke: 204, listed as revoked, the Clerk invitation withdrawn, signup refused
	if code, b = s.call(t, "DELETE", "/api/invitations/"+inv.ID, admin, ""); code != 204 {
		t.Fatalf("revoke: %d %s", code, b)
	}
	if !s.mock.Invitations()[2].Revoked {
		t.Fatal("the Clerk invitation was not revoked")
	}
	_, lb := s.call(t, "GET", "/api/invitations", admin, "")
	var seen bool
	for _, i := range decodeAs[contract.InvitationList](t, lb).Items {
		if i.ID == inv.ID {
			seen = true
			if i.Status != contract.InvitationRevoked {
				t.Fatalf("status %s, want revoked", i.Status)
			}
		}
	}
	if !seen {
		t.Fatal("revoked invitation not listed")
	}
	code, b = s.call(t, "POST", "/api/auth/complete-signup", s.mock.Token(clerkmock.Claims{Subject: "user_dev", Email: "dev@example.test"}), "")
	wantErr(t, code, b, 403, contract.CodeNotInvited)

	// a revoked, accepted or unknown invitation cannot be revoked or resent again
	for _, r := range [][2]string{{"DELETE", "/api/invitations/" + inv.ID}, {"POST", "/api/invitations/" + inv.ID + "/resend"},
		{"DELETE", "/api/invitations/00000000-0000-0000-0000-000000000001"}, {"POST", "/api/invitations/nope/resend"}} {
		code, b = s.call(t, r[0], r[1], admin, "")
		wantErr(t, code, b, 404, contract.CodeNotFound)
	}
	// and the email can be invited anew
	if code, b = s.call(t, "POST", "/api/invitations", admin, `{"email":"dev@example.test","role":"admin"}`); code != 201 {
		t.Fatalf("re-invite after revoke: %d %s", code, b)
	}
}

func TestInvitationBodiesAreValidated(t *testing.T) {
	s := newStack(t)
	admin := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	tests := map[string]string{
		"no email":          `{"role":"member"}`,
		"not an email":      `{"email":"nope","role":"member"}`,
		"display name form": `{"email":"Ada <ada@example.test>","role":"member"}`,
		"two addresses":     `{"email":"a@example.test, b@example.test","role":"member"}`,
		"bad role":          `{"email":"a@example.test","role":"owner"}`,
		"no role":           `{"email":"a@example.test"}`,
		"unknown field":     `{"email":"a@example.test","role":"member","force":true}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			code, b := s.call(t, "POST", "/api/invitations", admin, body)
			wantErr(t, code, b, 422, contract.CodeValidationFailed)
		})
	}
	if n := len(s.mock.Invitations()); n != 1 { // only the bootstrap one
		t.Fatalf("an invalid request reached Clerk: %d invitations", n)
	}
}

func TestInvitationFailsCleanlyWhenClerkIsDown(t *testing.T) {
	s := newStack(t)
	admin := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	s.mock.SetAPIDown(true)
	code, b := s.call(t, "POST", "/api/invitations", admin, `{"email":"a@example.test","role":"member"}`)
	wantErr(t, code, b, 503, contract.CodeUnavailable)
	s.mock.SetAPIDown(false)
	// nothing was recorded, so the same invitation can be created once Clerk is back
	if code, b = s.call(t, "POST", "/api/invitations", admin, `{"email":"a@example.test","role":"member"}`); code != 201 {
		t.Fatalf("after recovery: %d %s", code, b)
	}
}

func TestAnExpiredInvitationCannotBeAcceptedAndCanBeReplaced(t *testing.T) {
	s := newStack(t)
	admin := s.signInAs(t, "user_admin", "admin@example.test", contract.RoleAdmin)
	_, b := s.call(t, "POST", "/api/invitations", admin, `{"email":"late@example.test","role":"member"}`)
	inv := decodeAs[contract.InvitationView](t, b)
	if _, err := s.db.Pool().Exec(t.Context(), `UPDATE invitations SET expires_at = now() - interval '1 hour' WHERE id = $1`, inv.ID); err != nil {
		t.Fatal(err)
	}
	code, b := s.call(t, "POST", "/api/auth/complete-signup", s.mock.Token(clerkmock.Claims{Subject: "user_late", Email: "late@example.test"}), "")
	wantErr(t, code, b, 403, contract.CodeNotInvited)
	_, lb := s.call(t, "GET", "/api/invitations", admin, "")
	for _, i := range decodeAs[contract.InvitationList](t, lb).Items {
		if i.ID == inv.ID && i.Status != contract.InvitationExpired {
			t.Fatalf("status %s, want expired", i.Status)
		}
	}
	if code, b = s.call(t, "POST", "/api/invitations", admin, `{"email":"late@example.test","role":"member"}`); code != 201 {
		t.Fatalf("replacing an expired invitation: %d %s", code, b)
	}
}
