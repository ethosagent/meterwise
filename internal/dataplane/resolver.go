package dataplane

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/wsauth"
)

// refusal is a resolver or admission error that picks its HTTP status. tollgate writes the body
// for the provider the path names (ADR 0008), so the text here is for logs and tests only.
type refusal struct {
	status int
	msg    string
}

func (e *refusal) Error() string   { return e.msg }
func (e *refusal) HTTPStatus() int { return e.status }

var (
	errUnknownProvider = &refusal{http.StatusNotFound, "dataplane: unknown provider"}
	errNoWorkspaceURL  = &refusal{http.StatusNotFound, "dataplane: a workspace url is required"}
	errProviderDenied  = &refusal{http.StatusForbidden, "dataplane: provider not allowed for this workspace"}
)

// DefaultProviders is the provider table tollgate's own prefix resolver uses (its table is
// internal, so it is mirrored here; the data-plane tests pin both against the testkit fake).
func DefaultProviders() map[string]tollgate.Provider {
	return map[string]tollgate.Provider{
		"openai": {Name: "openai", Wire: tollgate.WireOpenAIChat,
			BaseURL: &url.URL{Scheme: "https", Host: "api.openai.com"}},
		"anthropic": {Name: "anthropic", Wire: tollgate.WireAnthropicMessages,
			BaseURL: &url.URL{Scheme: "https", Host: "api.anthropic.com"}},
		"openrouter": {Name: "openrouter", Wire: tollgate.WireOpenAIChat,
			BaseURL: &url.URL{Scheme: "https", Host: "openrouter.ai", Path: "/api"}},
	}
}

// WorkspaceResolver implements tollgate.ProviderResolver for /w/{id}/{provider}/{rest}: it drops
// the workspace prefix (wsauth.Front already did when the request came through it) and picks the
// provider by the first path segment, like tollgate's default prefix resolver. A provider outside
// the workspace's Policy.AllowedProviders is a 403. A path with no workspace prefix is a 404
// unless the resolver was built for dev.
type WorkspaceResolver struct {
	providers map[string]tollgate.Provider
	allowBare bool
}

var _ tollgate.ProviderResolver = (*WorkspaceResolver)(nil)

// NewWorkspaceResolver builds the resolver over providers (nil means DefaultProviders). allowBare
// is true only for APP_ENV=dev.
func NewWorkspaceResolver(providers map[string]tollgate.Provider, allowBare bool) *WorkspaceResolver {
	if providers == nil {
		providers = DefaultProviders()
	}
	return &WorkspaceResolver{providers: providers, allowBare: allowBare}
}

// InlineSafe: Resolve is a prefix strip and a map lookup.
func (*WorkspaceResolver) InlineSafe() {}

// Resolve implements tollgate.ProviderResolver.
func (r *WorkspaceResolver) Resolve(_ context.Context, t tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	path := req.URL.Path
	if _, rest, ok := wsauth.Parse(path); ok {
		path = rest
	} else if t.Attrs[wsauth.AttrPublicID] == "" && !r.allowBare {
		return tollgate.Provider{}, "", errNoWorkspaceURL
	}
	seg, rest, _ := strings.Cut(strings.TrimPrefix(path, "/"), "/")
	p, ok := r.providers[seg]
	if !ok {
		return tollgate.Provider{}, "", errUnknownProvider
	}
	if t.Policy != nil && len(t.Policy.AllowedProviders) > 0 && !slices.Contains(t.Policy.AllowedProviders, p.Name) {
		return tollgate.Provider{}, "", errProviderDenied
	}
	return p, "/" + rest, nil
}
