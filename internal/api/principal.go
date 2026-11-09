package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// PrincipalResolver turns an HTTP request into the Session of the caller. It is
// the only way a caller enters a request. A typed *contract.Error is answered as
// is, a store failure is 503 unavailable, and any other error means "not
// authenticated" (401): the gateway fails closed.
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

// gateway is the one place a session is attached and the one role check of
// Plan 02 (replaced by RBAC in Plan 03): the route's permission decides, handlers
// never look at the role. PermSelf needs only a valid, undeleted user; anything
// else needs an active membership (403 not_invited otherwise) and a role that
// contract.Allows (403 forbidden).
func gateway(res PrincipalResolver, perm contract.Permission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, err := res.Resolve(r)
		if err != nil {
			writeError(w, resolveError(err))
			return
		}
		if perm != contract.PermSelf {
			if !sess.Member {
				writeError(w, contract.NewError(contract.CodeNotInvited, "this account has not been invited to the organization"))
				return
			}
			if !contract.Allows(sess.Principal.Role, perm) {
				writeError(w, contract.NewError(contract.CodeForbidden, "your role cannot do this"))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)))
	})
}

func resolveError(err error) *contract.Error {
	var typed *contract.Error
	switch {
	case errors.As(err, &typed):
		return typed
	case store.ClassOf(err) != "":
		return contract.NewError(contract.CodeUnavailable, "a dependency is not reachable right now; retry")
	}
	return contract.NewError(contract.CodeUnauthenticated, "authentication required")
}
