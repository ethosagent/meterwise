package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
)

func (s *stack) webhook(t *testing.T, h http.Header, body []byte) (int, []byte) {
	t.Helper()
	return s.callWith(t, "POST", "/api/webhooks/clerk", string(body), func(r *http.Request) {
		for k, v := range h {
			r.Header[k] = v
		}
	})
}

func (s *stack) signed(t *testing.T, fixture, msgID string, ts time.Time) (http.Header, []byte) {
	t.Helper()
	body, err := clerkmock.Fixture(fixture)
	if err != nil {
		t.Fatal(err)
	}
	return s.mock.SignWebhook(msgID, ts, body), body
}

func (s *stack) userRow(t *testing.T, clerkID string) (email, name string, deleted bool, found bool) {
	t.Helper()
	err := s.db.Pool().QueryRow(context.Background(),
		`SELECT email, name, deleted_at IS NOT NULL FROM users WHERE clerk_user_id = $1`, clerkID).Scan(&email, &name, &deleted)
	return email, name, deleted, err == nil
}

func TestWebhookUpsertsUpdatesAndSoftDeletesUsers(t *testing.T) {
	s := newStack(t)
	const clerkID = "user_2fixtureAAA"
	for i, step := range []struct{ fixture, wantName string }{{"user.created", "Ada Lovelace"}, {"user.updated", "Ada Byron"}} {
		h, body := s.signed(t, step.fixture, "msg_"+step.fixture, time.Now())
		if code, b := s.webhook(t, h, body); code != 204 || len(b) != 0 {
			t.Fatalf("%s: %d %s", step.fixture, code, b)
		}
		email, name, deleted, found := s.userRow(t, clerkID)
		if !found || deleted || name != step.wantName || email != "ada@example.test" {
			t.Fatalf("step %d: row = %q %q deleted=%v found=%v", i, email, name, deleted, found)
		}
	}
	// an invited user who is deleted in Clerk loses access at once
	if _, _, err := s.svc.BootstrapAdmin(context.Background(), "ada@example.test"); err != nil {
		t.Fatal(err)
	}
	tok := s.mock.Token(clerkmock.Claims{Subject: clerkID, Email: "ada@example.test"})
	if code, b := s.call(t, "POST", "/api/auth/complete-signup", tok, ""); code != 200 {
		t.Fatalf("signup: %d %s", code, b)
	}
	h, body := s.signed(t, "user.deleted", "msg_deleted", time.Now())
	if code, b := s.webhook(t, h, body); code != 204 {
		t.Fatalf("delete: %d %s", code, b)
	}
	if _, _, deleted, _ := s.userRow(t, clerkID); !deleted {
		t.Fatal("user not soft-deleted")
	}
	code, b := s.call(t, "GET", "/api/me", tok, "")
	wantErr(t, code, b, 401, contract.CodeUnauthenticated)
	// the row is kept (soft delete) and a late valid token cannot resurrect it
	if _, _, deleted, found := s.userRow(t, clerkID); !found || !deleted {
		t.Fatal("the soft-deleted user was resurrected by a login")
	}
}

func TestLoginWorksWithZeroWebhooks(t *testing.T) {
	s := newStack(t)
	tok := s.signInAs(t, "user_never_webhooked", "solo@example.test", contract.RoleAdmin)
	if code, b := s.call(t, "GET", "/api/me", tok, ""); code != 200 {
		t.Fatalf("%d %s", code, b)
	}
	if email, _, _, found := s.userRow(t, "user_never_webhooked"); !found || email != "solo@example.test" {
		t.Fatalf("lazy upsert missing: %q %v", email, found)
	}
}

func TestBadWebhooksAreRejectedAndChangeNothing(t *testing.T) {
	s := newStack(t)
	now := time.Now()
	good, body := s.signed(t, "user.created", "msg_ok", now)
	other := http.Header{}
	for k, v := range good {
		other[k] = append([]string(nil), v...)
	}
	other.Set("svix-signature", "v1,AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	stale, staleBody := s.signed(t, "user.created", "msg_stale", now.Add(-10*time.Minute))
	foreign := clerkmock.SignWebhook("whsec_ZGlmZmVyZW50LXNlY3JldC1rZXk=", "msg_foreign", now, body)
	tests := []struct {
		name string
		h    http.Header
		body []byte
	}{
		{"no signature headers", http.Header{}, body},
		{"bad signature", other, body},
		{"signed with another secret", foreign, body},
		{"stale timestamp (replayed capture)", stale, staleBody},
		{"body altered after signing", good, []byte(strings.Replace(string(body), "Ada", "Eve", 1))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, b := s.webhook(t, tc.h, tc.body)
			wantErr(t, code, b, 400, contract.CodeInvalidRequest)
		})
	}
	if _, _, _, found := s.userRow(t, "user_2fixtureAAA"); found {
		t.Fatal("a rejected webhook created a user")
	}
	// the genuine one still works, once; the identical replay is rejected
	if code, b := s.webhook(t, good, body); code != 204 {
		t.Fatalf("genuine: %d %s", code, b)
	}
	code, b := s.webhook(t, good, body)
	wantErr(t, code, b, 400, contract.CodeInvalidRequest)
}

func TestUnrelatedWebhookEventsAreAcknowledged(t *testing.T) {
	s := newStack(t)
	body := []byte(`{"type":"session.created","data":{"id":"sess_1"}}`)
	if code, b := s.webhook(t, s.mock.SignWebhook("msg_sess", time.Now(), body), body); code != 204 {
		t.Fatalf("%d %s", code, b)
	}
}
