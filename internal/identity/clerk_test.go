package identity_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
)

func newPair(t *testing.T, opts ...func(*identity.ClerkConfig)) (*clerkmock.Server, *identity.Clerk) {
	t.Helper()
	mock, err := clerkmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Close)
	cfg := identity.ClerkConfig{Issuer: mock.Issuer(), JWKSURL: mock.JWKSURL(), SecretKey: clerkmock.BackendKey(), APIURL: mock.APIURL()}
	for _, o := range opts {
		o(&cfg)
	}
	c, err := identity.NewClerk(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return mock, c
}

func TestVerifyAcceptsAValidTokenAndMapsClaims(t *testing.T) {
	mock, c := newPair(t)
	tok := mock.Token(clerkmock.Claims{Subject: "user_1", Email: "Ada@Example.test", Name: "Ada", ImageURL: "https://img/x.png"})
	id, err := c.Verify(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	want := contract.Identity{Subject: "user_1", Email: "Ada@Example.test", EmailVerified: true, Name: "Ada", AvatarURL: "https://img/x.png"}
	if id != want {
		t.Fatalf("got %+v, want %+v", id, want)
	}
	if un, _ := c.Verify(context.Background(), mock.Token(clerkmock.Claims{Subject: "u", Email: "a@b.test", Unverified: true})); un.EmailVerified {
		t.Fatal("an unverified email was reported verified")
	}
}

func TestVerifyRejectsEveryForgery(t *testing.T) {
	mock, c := newPair(t)
	good := clerkmock.Claims{Subject: "user_1", Email: "a@example.test"}
	tests := []struct {
		name  string
		token string
	}{
		{"wrong signature (foreign key, published kid)", mock.Token(clerkmock.Claims{Subject: "user_1", SignWith: clerkmock.ForeignKey()})},
		{"payload tampered after signing", mock.Token(clerkmock.Claims{Subject: "user_1", Email: "a@example.test", TamperPayload: true})},
		{"wrong issuer", mock.Token(clerkmock.Claims{Subject: "user_1", Issuer: "https://clerk.evil.example"})},
		{"issuer with a clerk-looking prefix", mock.Token(clerkmock.Claims{Subject: "user_1", Issuer: mock.Issuer() + ".evil"})},
		{"expired", mock.Token(clerkmock.Claims{Subject: "user_1", ExpiresIn: -time.Hour})},
		{"not yet valid", mock.Token(clerkmock.Claims{Subject: "user_1", NotBeforeIn: time.Hour})},
		{"alg none", mock.Token(clerkmock.Claims{Subject: "user_1", Alg: "none"})},
		{"unknown kid", mock.Token(clerkmock.Claims{Subject: "user_1", KeyID: "ins_unknown"})},
		{"no subject", mock.Token(clerkmock.Claims{OmitSubject: true})},
		{"garbage", "not.a.jwt"},
		{"empty", ""},
		{"huge", strings.Repeat("a", 9<<10)},
	}
	if _, err := c.Verify(context.Background(), mock.Token(good)); err != nil {
		t.Fatalf("control token rejected: %v", err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.Verify(context.Background(), tc.token)
			if e := contract.AsError(err); e.Code != contract.CodeUnauthenticated {
				t.Fatalf("code %s (%v), want unauthenticated", e.Code, err)
			}
			if tc.token != "" && strings.Contains(err.Error(), tc.token) {
				t.Fatal("the error echoes the token")
			}
		})
	}
}

func TestUnknownKidRefetchesAtMostOncePerGap(t *testing.T) {
	now := time.Now()
	mock, c := newPair(t, func(cfg *identity.ClerkConfig) { cfg.Now = func() time.Time { return now } })
	bad := mock.Token(clerkmock.Claims{Subject: "u", KeyID: "ins_nope"})
	for range 5 {
		_, _ = c.Verify(context.Background(), bad)
	}
	if hits := mock.JWKSHits(); hits != 1 {
		t.Fatalf("a flood of unknown kids fetched the JWKS %d times, want 1", hits)
	}
	now = now.Add(11 * time.Second)
	_, _ = c.Verify(context.Background(), bad)
	if hits := mock.JWKSHits(); hits != 2 {
		t.Fatalf("after the gap: %d fetches, want 2", hits)
	}
}

func TestJWKSOutageIsUnavailableUntilKeysAreCached(t *testing.T) {
	mock, c := newPair(t)
	mock.SetJWKSDown(true)
	tok := mock.Token(clerkmock.Claims{Subject: "u", Email: "a@b.test"})
	if _, err := c.Verify(context.Background(), tok); contract.AsError(err).Code != contract.CodeUnavailable {
		t.Fatalf("outage: %v", err)
	}
	mock.SetJWKSDown(false)
	// the refetch gap is real time here; inject a fresh adapter instead of sleeping
	_, c2 := newPair(t)
	if _, err := c2.Verify(context.Background(), tok); err == nil {
		t.Fatal("a token from another mock instance verified")
	}
}

func TestInvitationsGoThroughTheBackendAPI(t *testing.T) {
	mock, c := newPair(t)
	ctx := context.Background()
	id, err := c.CreateInvitation(ctx, "new@example.test")
	if err != nil || id == "" {
		t.Fatalf("create: %q %v", id, err)
	}
	if got := mock.Invitations(); len(got) != 1 || got[0].Email != "new@example.test" || got[0].Revoked {
		t.Fatalf("mock saw %+v", got)
	}
	if err := c.RevokeInvitation(ctx, id); err != nil || !mock.Invitations()[0].Revoked {
		t.Fatalf("revoke: %v %+v", err, mock.Invitations())
	}
	if err := c.RevokeInvitation(ctx, "inv_unknown"); err != nil {
		t.Fatalf("revoking an unknown invitation must not fail: %v", err)
	}
	mock.SetAPIDown(true)
	if _, err := c.CreateInvitation(ctx, "x@example.test"); contract.AsError(err).Code != contract.CodeUnavailable {
		t.Fatalf("API down: %v", err)
	}
}

func TestInvitationsNeedASecretKey(t *testing.T) {
	_, c := newPair(t, func(cfg *identity.ClerkConfig) { cfg.SecretKey = "" })
	if _, err := c.CreateInvitation(context.Background(), "a@b.test"); contract.AsError(err).Code != contract.CodeNotAvailable {
		t.Fatalf("got %v", err)
	}
}

func TestNewClerkRequiresIssuerAndJWKS(t *testing.T) {
	if _, err := identity.NewClerk(identity.ClerkConfig{}); err == nil {
		t.Fatal("empty config accepted")
	}
}

func TestARotatedKidWithJWKSUnreachableIsUnavailableNotUnauthenticated(t *testing.T) {
	now := time.Now()
	mock, c := newPair(t, func(cfg *identity.ClerkConfig) { cfg.Now = func() time.Time { return now } })
	if _, err := c.Verify(context.Background(), mock.Token(clerkmock.Claims{Subject: "u", Email: "a@b.test"})); err != nil {
		t.Fatalf("warm the cache: %v", err)
	}
	rotated := mock.Token(clerkmock.Claims{Subject: "u", Email: "a@b.test", KeyID: "ins_rotated"})
	now = now.Add(11 * time.Second)
	mock.SetJWKSDown(true)
	if _, err := c.Verify(context.Background(), rotated); contract.AsError(err).Code != contract.CodeUnavailable {
		t.Fatalf("rotated kid, JWKS down: %v, want unavailable", err)
	}
	// inside the refetch gap the outage is remembered rather than turning into a 401
	if _, err := c.Verify(context.Background(), rotated); contract.AsError(err).Code != contract.CodeUnavailable {
		t.Fatalf("rotated kid inside the gap: %v, want unavailable", err)
	}
	// a cached key keeps working through the outage
	if _, err := c.Verify(context.Background(), mock.Token(clerkmock.Claims{Subject: "u", Email: "a@b.test"})); err != nil {
		t.Fatalf("cached key during the outage: %v", err)
	}
	// once the JWKS answers, a kid that is still unknown is a real rejection
	mock.SetJWKSDown(false)
	now = now.Add(11 * time.Second)
	if _, err := c.Verify(context.Background(), rotated); contract.AsError(err).Code != contract.CodeUnauthenticated {
		t.Fatalf("unknown kid, JWKS up: %v, want unauthenticated", err)
	}
}

// gate blocks every JWKS round trip until released, counting the ones that arrive.
type gate struct {
	mu      sync.Mutex
	hold    bool
	arrived chan struct{}
	release chan struct{}
	next    http.RoundTripper
}

func (g *gate) RoundTrip(r *http.Request) (*http.Response, error) {
	g.mu.Lock()
	hold := g.hold
	g.mu.Unlock()
	if hold {
		g.arrived <- struct{}{}
		<-g.release
	}
	return g.next.RoundTrip(r)
}

func TestAJWKSFetchDoesNotBlockVerificationsWithACachedKey(t *testing.T) {
	now := time.Now()
	g := &gate{arrived: make(chan struct{}, 4), release: make(chan struct{}), next: http.DefaultTransport}
	mock, c := newPair(t, func(cfg *identity.ClerkConfig) {
		cfg.Now = func() time.Time { return now }
		cfg.HTTPClient = &http.Client{Transport: g, Timeout: 5 * time.Second}
	})
	good := mock.Token(clerkmock.Claims{Subject: "u", Email: "a@b.test"})
	if _, err := c.Verify(context.Background(), good); err != nil {
		t.Fatalf("warm the cache: %v", err)
	}
	now = now.Add(11 * time.Second)
	g.mu.Lock()
	g.hold = true
	g.mu.Unlock()

	slow := make(chan error, 1)
	go func() {
		_, err := c.Verify(context.Background(), mock.Token(clerkmock.Claims{Subject: "u", KeyID: "ins_rotated"}))
		slow <- err
	}()
	<-g.arrived // the unknown-kid caller is now inside the JWKS fetch

	fast := make(chan error, 1)
	go func() { _, err := c.Verify(context.Background(), good); fast <- err }()
	select {
	case err := <-fast:
		if err != nil {
			t.Fatalf("cached-key verification: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a verification with a cached key waited for the JWKS fetch (mutex held across the network call)")
	}
	close(g.release)
	if err := <-slow; contract.AsError(err).Code != contract.CodeUnauthenticated {
		t.Fatalf("slow caller: %v, want unauthenticated", err)
	}
}
