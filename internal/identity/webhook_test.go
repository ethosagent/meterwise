package identity_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
)

func newVerifier(t *testing.T, now *time.Time) (*identity.WebhookVerifier, string) {
	t.Helper()
	mock, err := clerkmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	v, err := identity.NewWebhookVerifier(mock.WebhookSecret(), func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	return v, mock.WebhookSecret()
}

func TestWebhookFixturesVerifyAndParse(t *testing.T) {
	now := time.Now()
	v, secret := newVerifier(t, &now)
	tests := []struct {
		fixture string
		check   func(identity.WebhookEvent) bool
	}{
		{"user.created", func(e identity.WebhookEvent) bool {
			return e.User.ClerkUserID == "user_2fixtureAAA" && e.User.Email == "Ada@Example.test" && e.User.EmailVerified && e.User.Name == "Ada Lovelace"
		}},
		{"user.updated", func(e identity.WebhookEvent) bool { return e.User.Name == "Ada Byron" && e.User.AvatarURL != "" }},
		{"user.deleted", func(e identity.WebhookEvent) bool {
			return e.User.ClerkUserID == "user_2fixtureAAA" && e.Type == "user.deleted"
		}},
	}
	for _, tc := range tests {
		t.Run(tc.fixture, func(t *testing.T) {
			body, err := clerkmock.Fixture(tc.fixture)
			if err != nil {
				t.Fatal(err)
			}
			ev, err := v.Verify(body, clerkmock.SignWebhook(secret, "msg_"+tc.fixture, now, body))
			if err != nil || ev.Type != tc.fixture || !tc.check(ev) {
				t.Fatalf("%+v, %v", ev, err)
			}
		})
	}
}

func TestWebhookRejections(t *testing.T) {
	now := time.Now()
	v, secret := newVerifier(t, &now)
	body, _ := clerkmock.Fixture("user.created")
	sign := func(id string, ts time.Time) http.Header { return clerkmock.SignWebhook(secret, id, ts, body) }
	mutate := func(h http.Header, f func(http.Header)) http.Header { f(h); return h }
	tests := []struct {
		name string
		body []byte
		h    http.Header
	}{
		{"no headers", body, http.Header{}},
		{"bad signature", body, mutate(sign("m1", now), func(h http.Header) { h.Set("svix-signature", "v1,AAAA") })},
		{"signed with another secret", body, clerkmock.SignWebhook("whsec_ZGlmZmVyZW50LXNlY3JldC1rZXk=", "m2", now, body)},
		{"body changed after signing", []byte(`{"type":"user.deleted","data":{"id":"user_2fixtureAAA"}}`), sign("m3", now)},
		{"too old (replayed capture)", body, sign("m4", now.Add(-6*time.Minute))},
		{"too new", body, sign("m5", now.Add(6*time.Minute))},
		{"timestamp header altered", body, mutate(sign("m6", now), func(h http.Header) { h.Set("svix-timestamp", "1") })},
		{"id header altered", body, mutate(sign("m7", now), func(h http.Header) { h.Set("svix-id", "m7x") })},
		{"not an event", []byte(`{}`), clerkmock.SignWebhook(secret, "m8", now, []byte(`{}`))},
		{"user event without id", []byte(`{"type":"user.created","data":{}}`), clerkmock.SignWebhook(secret, "m9", now, []byte(`{"type":"user.created","data":{}}`))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := v.Verify(tc.body, tc.h); !errors.Is(err, identity.ErrWebhook) {
				t.Fatalf("got %v, want ErrWebhook", err)
			}
		})
	}
}

func TestWebhookReplayOfAProcessedMessageIsRejectedButARetryIsNot(t *testing.T) {
	now := time.Now()
	v, secret := newVerifier(t, &now)
	body, _ := clerkmock.Fixture("user.created")
	h := clerkmock.SignWebhook(secret, "msg_once", now, body)
	if _, err := v.Verify(body, h); err != nil {
		t.Fatal(err)
	}
	// not marked processed (the first delivery failed): Svix retries with the same id
	if _, err := v.Verify(body, h); err != nil {
		t.Fatalf("a retry of an unprocessed message was refused: %v", err)
	}
	v.MarkProcessed("msg_once")
	if _, err := v.Verify(body, h); !errors.Is(err, identity.ErrWebhook) {
		t.Fatalf("a replay of a processed message was accepted: %v", err)
	}
}

func TestUnknownEventTypesVerifyWithoutUser(t *testing.T) {
	now := time.Now()
	v, secret := newVerifier(t, &now)
	body := []byte(`{"type":"session.created","data":{"id":"sess_1"}}`)
	ev, err := v.Verify(body, clerkmock.SignWebhook(secret, "m_s", now, body))
	if err != nil || ev.Type != "session.created" || ev.User.ClerkUserID != "" {
		t.Fatalf("%+v %v", ev, err)
	}
}

func TestNewWebhookVerifierRejectsAnInvalidSecret(t *testing.T) {
	if _, err := identity.NewWebhookVerifier("whsec_!!!notbase64", nil); err == nil {
		t.Fatal("invalid secret accepted")
	}
}
