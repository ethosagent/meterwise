package identity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/invitation"
	"github.com/clerk/clerk-sdk-go/v2/jwt"

	"github.com/ethosagent/meterwise/internal/contract"
)

const (
	jwksTimeout       = 5 * time.Second
	jwksMaxBytes      = 1 << 20
	jwksRefetchGap    = 10 * time.Second // an unknown kid refetches at most this often
	jwksMaxAge        = time.Hour
	defaultLeeway     = 5 * time.Second
	invitationDays    = 7
	maxTokenBytes     = 8 << 10
	invitationTimeout = 10 * time.Second
)

// InvitationLifetime is how long an invitation stays acceptable.
const InvitationLifetime = invitationDays * 24 * time.Hour

// ClerkConfig configures the adapter. Issuer and JWKSURL are required.
type ClerkConfig struct {
	Issuer      string // exact "iss" a token must carry
	JWKSURL     string // where the signing keys live
	SecretKey   string // backend API key; empty disables invitations
	APIURL      string // backend API base, default https://api.clerk.com/v1
	RedirectURL string // where an invitation email lands (the accept-invite page); optional
	HTTPClient  *http.Client
	Now         func() time.Time // injected clock; default time.Now
	Leeway      time.Duration    // clock skew allowed on exp/nbf; default 5s
	// AuthorizedParties are the origins a token's azp may name. Empty disables
	// the check (dev only; config validation requires it elsewhere).
	AuthorizedParties []string
}

// Clerk implements contract.IdentityProvider against a Clerk instance.
type Clerk struct {
	cfg ClerkConfig
	inv *invitation.Client

	mu        sync.Mutex
	keys      map[string]*clerk.JSONWebKey
	fetchedAt time.Time
	lastTry   time.Time
	fetchFail bool          // the most recent JWKS fetch failed
	inflight  chan struct{} // closed when the fetch in progress ends; nil when none
}

var _ contract.IdentityProvider = (*Clerk)(nil)

// NewClerk validates the config and builds the adapter. It makes no network call.
func NewClerk(cfg ClerkConfig) (*Clerk, error) {
	if cfg.Issuer == "" || cfg.JWKSURL == "" {
		return nil, errors.New("identity: Issuer and JWKSURL are required")
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: jwksTimeout}
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Leeway == 0 {
		cfg.Leeway = defaultLeeway
	}
	if cfg.APIURL == "" {
		cfg.APIURL = clerk.APIURL
	}
	c := &Clerk{cfg: cfg, keys: map[string]*clerk.JSONWebKey{}}
	if cfg.SecretKey != "" {
		c.inv = invitation.NewClient(&clerk.ClientConfig{BackendConfig: clerk.BackendConfig{
			HTTPClient: &http.Client{Timeout: invitationTimeout, Transport: cfg.HTTPClient.Transport},
			URL:        &cfg.APIURL, Key: &cfg.SecretKey,
		}})
	}
	return c, nil
}

type clock struct{ now func() time.Time }

func (c clock) Now() time.Time { return c.now() }

// sessionClaims are the custom claims the Clerk session token template adds.
type sessionClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	ImageURL      string `json:"image_url"`
}

func unauthenticated() error {
	return contract.NewError(contract.CodeUnauthenticated, "invalid or expired token")
}

// Verify checks the token's signature against the JWKS (RS256 only, by key id),
// its issuer and its time claims. Every rejection is CodeUnauthenticated and
// the message never echoes the token. An unreachable JWKS with no usable cached
// key is CodeUnavailable.
func (c *Clerk) Verify(ctx context.Context, bearer string) (contract.Identity, error) {
	if bearer == "" || len(bearer) > maxTokenBytes {
		return contract.Identity{}, unauthenticated()
	}
	unverified, err := jwt.Decode(ctx, &jwt.DecodeParams{Token: bearer})
	if err != nil || unverified.KeyID == "" {
		return contract.Identity{}, unauthenticated()
	}
	key, err := c.key(ctx, unverified.KeyID)
	if err != nil {
		return contract.Identity{}, err
	}
	issuer := c.cfg.Issuer
	custom := &sessionClaims{}
	claims, err := jwt.Verify(ctx, &jwt.VerifyParams{
		Token: bearer, JWK: key, Clock: clock{c.cfg.Now}, Leeway: c.cfg.Leeway,
		ProxyURL:                &issuer,
		AuthorizedPartyHandler:  c.authorizedParty,
		CustomClaimsConstructor: func(context.Context) any { return custom },
	})
	if err != nil || claims.Issuer != c.cfg.Issuer || claims.Subject == "" || claims.Expiry == nil {
		return contract.Identity{}, unauthenticated()
	}
	return contract.Identity{
		Subject: claims.Subject, Email: strings.TrimSpace(custom.Email), EmailVerified: custom.EmailVerified && custom.Email != "",
		Name: custom.Name, AvatarURL: custom.ImageURL,
	}, nil
}

// authorizedParty is the azp check: with parties configured, the token must name
// one of them (a missing azp is refused); with none configured it passes.
func (c *Clerk) authorizedParty(azp string) bool {
	if len(c.cfg.AuthorizedParties) == 0 {
		return true
	}
	for _, p := range c.cfg.AuthorizedParties {
		if azp != "" && azp == p {
			return true
		}
	}
	return false
}

// key returns the signing key for kid, refreshing the JWKS when it is stale or
// the kid is unknown (rate-limited, so a flood of forged kids cannot hammer Clerk).
// The mutex is never held across the network call: one caller fetches, the others
// wait on it (or use a cached key straight away). An unknown kid while the last
// fetch failed is CodeUnavailable, not unauthenticated: a rotated key cannot be
// told apart from a forged one until the JWKS answers, and a retry may succeed.
func (c *Clerk) key(ctx context.Context, kid string) (*clerk.JSONWebKey, error) {
	c.mu.Lock()
	now := c.cfg.Now()
	if k, ok := c.keys[kid]; ok && now.Sub(c.fetchedAt) < jwksMaxAge {
		c.mu.Unlock()
		return k, nil
	}
	switch {
	case c.inflight != nil:
		done := c.inflight
		c.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, contract.NewError(contract.CodeUnavailable, "the identity provider's keys are unavailable")
		}
		c.mu.Lock()
	case now.Sub(c.lastTry) >= jwksRefetchGap:
		done := make(chan struct{})
		c.inflight, c.lastTry = done, now
		c.mu.Unlock()
		// The fetch outlives this caller's cancellation: others wait on it.
		keys, err := c.fetchKeys(context.WithoutCancel(ctx))
		c.mu.Lock()
		c.inflight = nil
		close(done)
		if err == nil {
			c.keys, c.fetchedAt, c.fetchFail = keys, now, false
		} else {
			c.fetchFail = true
		}
	}
	k, ok := c.keys[kid]
	failed := c.fetchFail
	c.mu.Unlock()
	switch {
	case ok:
		return k, nil
	case failed:
		return nil, contract.NewError(contract.CodeUnavailable, "the identity provider's keys are unavailable")
	}
	return nil, unauthenticated()
}

func (c *Clerk) fetchKeys(ctx context.Context) (map[string]*clerk.JSONWebKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.JWKSURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("jwks: unexpected status")
	}
	var set struct {
		Keys []*clerk.JSONWebKey `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, jwksMaxBytes)).Decode(&set); err != nil {
		return nil, err
	}
	keys := make(map[string]*clerk.JSONWebKey, len(set.Keys))
	for _, k := range set.Keys {
		if k != nil && k.KeyID != "" && k.Key != nil {
			keys[k.KeyID] = k
		}
	}
	return keys, nil
}

func (c *Clerk) providerDown() error {
	return contract.NewError(contract.CodeUnavailable, "the identity provider could not complete the request")
}

// CreateInvitation asks Clerk to email an invitation (sign-ups stay disabled, so
// this is the only way in) and returns Clerk's invitation id.
func (c *Clerk) CreateInvitation(ctx context.Context, email string) (string, error) {
	if c.inv == nil {
		return "", contract.NewError(contract.CodeNotAvailable, "CLERK_SECRET_KEY is not set, so invitations cannot be sent")
	}
	days, notify, ignore := int64(invitationDays), true, true
	p := &invitation.CreateParams{EmailAddress: email, ExpiresInDays: &days, Notify: &notify, IgnoreExisting: &ignore}
	if c.cfg.RedirectURL != "" {
		p.RedirectURL = &c.cfg.RedirectURL
	}
	inv, err := c.inv.Create(ctx, p)
	if err != nil || inv.ID == "" {
		return "", c.providerDown()
	}
	return inv.ID, nil
}

// RevokeInvitation revokes a pending Clerk invitation; one Clerk no longer knows is not an error.
func (c *Clerk) RevokeInvitation(ctx context.Context, id string) error {
	if id == "" {
		return nil
	}
	if c.inv == nil {
		return contract.NewError(contract.CodeNotAvailable, "CLERK_SECRET_KEY is not set, so invitations cannot be revoked")
	}
	if _, err := c.inv.Revoke(ctx, id); err != nil {
		var api *clerk.APIErrorResponse
		if errors.As(err, &api) && (api.HTTPStatusCode == http.StatusNotFound || api.HTTPStatusCode == http.StatusBadRequest) {
			return nil
		}
		return c.providerDown()
	}
	return nil
}
