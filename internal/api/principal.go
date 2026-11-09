package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
)

// PrincipalResolver turns an HTTP request into the Session of the caller. It is
// the only way a caller enters a request. Every error goes through the same
// mapper as the handlers (apiError): a typed *contract.Error is answered as is,
// an unreachable database is 503, and anything else is a logged 500. Only a
// typed unauthenticated answers 401, so a bug in Resolve is never a silent 401.
type PrincipalResolver interface {
	Resolve(r *http.Request) (contract.Session, error)
}

// ClerkPrincipalResolver reads the bearer token, verifies it with the identity
// provider and lets the tenancy layer mirror the user and attach the membership.
type ClerkPrincipalResolver struct {
	Provider contract.IdentityProvider
	Sessions contract.PrincipalResolver
}

// Resolve authenticates the request. No cookies are read (interface contract).
func (c ClerkPrincipalResolver) Resolve(r *http.Request) (contract.Session, error) {
	token, ok := bearerToken(r)
	if !ok {
		return contract.Session{}, contract.NewError(contract.CodeUnauthenticated, "authentication required")
	}
	id, err := c.Provider.Verify(r.Context(), token)
	if err != nil {
		return contract.Session{}, err
	}
	return c.Sessions.Resolve(r.Context(), id)
}

func bearerToken(r *http.Request) (string, bool) {
	scheme, token, found := strings.Cut(r.Header.Get("Authorization"), " ")
	token = strings.TrimSpace(token)
	if !found || !strings.EqualFold(scheme, "Bearer") || token == "" {
		return "", false
	}
	return token, true
}

// StubPrincipalResolver authenticates every request as one fixed dev admin whose
// session Load returns. It exists so the web app can run without a Clerk
// instance, and it cannot be built outside APP_ENV=dev: NewStubPrincipalResolver
// refuses, so production has no way to hold the insecure object.
type StubPrincipalResolver struct {
	load func(context.Context) (contract.Session, error)
}

// NewStubPrincipalResolver builds the dev-only resolver, or fails outside dev.
func NewStubPrincipalResolver(appEnv string, load func(context.Context) (contract.Session, error)) (*StubPrincipalResolver, error) {
	if appEnv != "dev" {
		return nil, errors.New("a stub resolver signs every request in as an admin and runs only with " + config.EnvAppEnv + "=dev")
	}
	return &StubPrincipalResolver{load: load}, nil
}

// Resolve returns the dev admin's session.
func (s *StubPrincipalResolver) Resolve(r *http.Request) (contract.Session, error) {
	return s.load(r.Context())
}

type sessionKey struct{}

// withSession attaches the session the gateway resolved; the only writer of the context key.
func withSession(ctx context.Context, s contract.Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, s)
}

// SessionFrom returns the session the gateway attached; ok is false on a public route.
func SessionFrom(ctx context.Context) (contract.Session, bool) {
	s, ok := ctx.Value(sessionKey{}).(contract.Session)
	return s, ok
}

// PrincipalFrom returns the principal of the caller when it holds an active membership.
func PrincipalFrom(ctx context.Context) (contract.Principal, bool) {
	s, ok := SessionFrom(ctx)
	return s.Principal, ok && s.Member
}
