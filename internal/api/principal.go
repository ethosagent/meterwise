package api

import (
	"context"
	"net/http"

	"github.com/ethosagent/meterwise/internal/contract"
)

// PrincipalResolver turns an HTTP request into the acting Principal. It is the
// only way a principal enters a request: Plan 01 ships StubPrincipalResolver,
// Plan 02 ships the Clerk-backed one. An error means "not authenticated" and
// becomes a 401 unauthorized body.
type PrincipalResolver interface {
	Resolve(r *http.Request) (contract.Principal, error)
}

// StubPrincipalResolver authenticates every request as one fixed admin. Dev
// only, until Plan 02.
type StubPrincipalResolver struct {
	OrgID  string
	UserID string
}

// Resolve returns the fixed admin principal.
func (s StubPrincipalResolver) Resolve(*http.Request) (contract.Principal, error) {
	return contract.Principal{UserID: s.UserID, OrgID: s.OrgID, Role: contract.RoleAdmin}, nil
}

type principalKey struct{}

// PrincipalFrom returns the principal the middleware attached; ok is false on
// a public route.
func PrincipalFrom(ctx context.Context) (contract.Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(contract.Principal)
	return p, ok
}

// withPrincipal is the one place a principal is attached to a request.
func withPrincipal(res PrincipalResolver, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := res.Resolve(r)
		if err != nil {
			writeError(w, contract.NewError(contract.CodeUnauthorized, "authentication required"))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, p)))
	})
}
