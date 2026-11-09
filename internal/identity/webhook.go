package identity

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	svix "github.com/svix/svix-webhooks/go"
)

// WebhookTolerance is how far a webhook timestamp may be from now; older or
// newer messages are rejected as replays.
const WebhookTolerance = 5 * time.Minute

// ErrWebhook is the only error Verify returns; it names no cause, because a
// forger learns nothing from knowing whether the signature, the timestamp or
// the replay check failed first.
var ErrWebhook = errors.New("identity: webhook rejected")

// WebhookUser is the user data of a user.* event.
type WebhookUser struct {
	ClerkUserID   string
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
}

// WebhookEvent is a verified Clerk event. User is set for user.* types.
type WebhookEvent struct {
	ID   string // the Svix message id
	Type string
	User WebhookUser
}

// WebhookVerifier checks the Svix signature, the timestamp window and replays.
type WebhookVerifier struct {
	wh  *svix.Webhook
	now func() time.Time

	mu   sync.Mutex
	seen map[string]time.Time // processed message id -> when
}

// NewWebhookVerifier builds a verifier from the Svix signing secret (whsec_...).
// now may be nil (time.Now); tests inject a clock.
func NewWebhookVerifier(secret string, now func() time.Time) (*WebhookVerifier, error) {
	wh, err := svix.NewWebhook(secret)
	if err != nil {
		return nil, errors.New("identity: CLERK_WEBHOOK_SECRET is not a valid Svix secret")
	}
	if now == nil {
		now = time.Now
	}
	return &WebhookVerifier{wh: wh, now: now, seen: map[string]time.Time{}}, nil
}

func messageID(h http.Header) string {
	if id := h.Get("svix-id"); id != "" {
		return id
	}
	return h.Get("webhook-id")
}

func messageTimestamp(h http.Header) string {
	if ts := h.Get("svix-timestamp"); ts != "" {
		return ts
	}
	return h.Get("webhook-timestamp")
}

// Verify authenticates payload and headers and parses the event. It rejects a bad
// or missing signature, a timestamp outside WebhookTolerance, and a message id
// already marked processed.
func (v *WebhookVerifier) Verify(payload []byte, h http.Header) (WebhookEvent, error) {
	secs, err := strconv.ParseInt(messageTimestamp(h), 10, 64)
	if err != nil {
		return WebhookEvent{}, ErrWebhook
	}
	now := v.now()
	if d := now.Sub(time.Unix(secs, 0)); d > WebhookTolerance || d < -WebhookTolerance {
		return WebhookEvent{}, ErrWebhook
	}
	if err := v.wh.VerifyIgnoringTimestamp(payload, h); err != nil {
		return WebhookEvent{}, ErrWebhook
	}
	id := messageID(h)
	v.mu.Lock()
	_, replay := v.seen[id]
	v.mu.Unlock()
	if replay {
		return WebhookEvent{}, ErrWebhook
	}
	ev, err := parseEvent(payload)
	if err != nil {
		return WebhookEvent{}, ErrWebhook
	}
	ev.ID = id
	return ev, nil
}

// MarkProcessed records that the message was applied, so a replay is refused.
// Call it after the event took effect: a Svix retry of a failed delivery reuses
// the id and must still be accepted.
func (v *WebhookVerifier) MarkProcessed(id string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	now := v.now()
	for k, at := range v.seen {
		if now.Sub(at) > 2*WebhookTolerance {
			delete(v.seen, k)
		}
	}
	v.seen[id] = now
}

type userData struct {
	ID                    string `json:"id"`
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name"`
	ImageURL              string `json:"image_url"`
	PrimaryEmailAddressID string `json:"primary_email_address_id"`
	EmailAddresses        []struct {
		ID           string `json:"id"`
		EmailAddress string `json:"email_address"`
		Verification struct {
			Status string `json:"status"`
		} `json:"verification"`
	} `json:"email_addresses"`
}

func parseEvent(payload []byte) (WebhookEvent, error) {
	var env struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil || env.Type == "" {
		return WebhookEvent{}, errors.New("not an event")
	}
	ev := WebhookEvent{Type: env.Type}
	if !strings.HasPrefix(env.Type, "user.") {
		return ev, nil
	}
	var d userData
	if err := json.Unmarshal(env.Data, &d); err != nil || d.ID == "" {
		return WebhookEvent{}, errors.New("user event without an id")
	}
	ev.User = WebhookUser{ClerkUserID: d.ID, Name: strings.TrimSpace(d.FirstName + " " + d.LastName), AvatarURL: d.ImageURL}
	for _, e := range d.EmailAddresses {
		if e.ID == d.PrimaryEmailAddressID {
			ev.User.Email, ev.User.EmailVerified = e.EmailAddress, e.Verification.Status == "verified"
		}
	}
	return ev, nil
}
