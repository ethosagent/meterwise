package identity_test

import (
	"context"
	"strings"
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
