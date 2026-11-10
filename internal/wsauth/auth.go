package wsauth

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// LookupTimeout bounds one directory lookup. A cache hit returns at once; a miss reads Postgres
// and must not hold the handler's goroutine, so it gives up and answers 503.
const LookupTimeout = 250 * time.Millisecond

// MetaURLRetired is the Call.Meta key (and the Tenant.Attrs key it is copied from) that marks a
// call made through a retired URL still inside its grace window. Its value is "true".
const MetaURLRetired = "url_retired"

// Tenant attribute keys.
const (
	AttrOrgID         = "org_id"
	AttrPublicID      = "public_id"
	AttrMaxConcurrent = "max_concurrent" // settings.maxConcurrent, when set and valid (decimal)
)

// Directory is the lookup the authenticator needs. tenancy.Directory implements it over its cache:
// CachedView answers a hit with no allocation and no deadline; LoadView is the miss path, one
// shared Postgres read bounded by ctx. Both return the view tenancy built with Resolve.
type Directory interface {
	CachedView(publicID string) (view any, ok bool, err error)
	LoadView(ctx context.Context, publicID string) (view any, err error)
}

// Error is an authentication failure that picks the HTTP status. tollgate maps the status and
// writes a provider-shaped body whose text depends on the status alone (ADR 0008), so the text
// here is for logs and tests: a 410 reads as "gone" to the client, and that is the signal for a
// reset URL.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// HTTPStatus implements tollgate.StatusError: the status the proxy answers with.
func (e *Error) HTTPStatus() int { return e.Status }

var (
	errUnknown     = &Error{Status: http.StatusNotFound, Msg: "wsauth: unknown workspace url"}
	errReset       = &Error{Status: http.StatusGone, Msg: "wsauth: this workspace url was reset"}
	errSourceIP    = &Error{Status: http.StatusForbidden, Msg: "wsauth: source address not allowed"}
	errUnavailable = &Error{Status: http.StatusServiceUnavailable, Msg: "wsauth: workspace lookup unavailable"}
)

// Authenticator implements tollgate.Authenticator over the workspace directory.
type Authenticator struct {
	dir     Directory
	timeout time.Duration
	dev     *tollgate.Tenant
}

var _ tollgate.Authenticator = (*Authenticator)(nil)

// New builds the authenticator; timeout 0 means LookupTimeout.
func New(dir Directory, timeout time.Duration) *Authenticator {
	if timeout <= 0 {
		timeout = LookupTimeout
	}
	return &Authenticator{dir: dir, timeout: timeout}
}

// WithDevTenant lets a request that carries no /w/{id} authenticate as t. Only APP_ENV=dev builds
// it: elsewhere a bare /{provider}/... path is 404.
func (a *Authenticator) WithDevTenant(t tollgate.Tenant) *Authenticator {
	c := *a
	c.dev = &t
	return &c
}

// InlineSafe marks Authenticate as non-blocking so tollgate runs it on the handler goroutine
// (ADR 0015). tollgate takes the marker per type, so it cannot be conditional on a cache hit: a
// hit is a map read, a miss does one directory read bounded by LookupTimeout. That bound is what
// keeps the claim honest; see docs/decisions/0017.
func (*Authenticator) InlineSafe() {}

// Authenticate resolves /w/{publicId} to the workspace's tenant. Unknown or malformed ids are 404,
// a reset id past its grace is 410, a source address outside the workspace's allowed networks is
// 403 and a lookup that fails or times out is 503. A retired id inside its grace window
// authenticates and carries Attrs["url_retired"]="true". It reads no credential header.
//
// A cache hit takes the caller's context as is and allocates nothing: the tenant is the one built
// when the entry was filled and is shared read-only. Only a miss gets a deadline (LookupTimeout).
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (tollgate.Tenant, error) {
	id, ok := requestID(ctx, r)
	if !ok {
		if a.dev != nil && !strings.HasPrefix(r.URL.Path, prefix) {
			return *a.dev, nil
		}
		return tollgate.Tenant{}, errUnknown
	}
	view, hit, err := a.dir.CachedView(id)
	if !hit {
		lctx, cancel := context.WithTimeout(ctx, a.timeout)
		view, err = a.dir.LoadView(lctx, id)
		cancel()
	}
	if err != nil {
		return tollgate.Tenant{}, refusal(err)
	}
	res, ok := view.(*Resolved)
	if !ok || res == nil {
		return tollgate.Tenant{}, errUnavailable
	}
	if !res.sourceAllowed(r.RemoteAddr) {
		return tollgate.Tenant{}, errSourceIP
	}
	return res.Tenant, nil
}

// refusal maps a directory error to the authentication failure that carries its HTTP status.
func refusal(err error) *Error {
	switch contract.AsError(err).Code {
	case contract.CodeNotFound:
		return errUnknown
	case contract.CodeWorkspaceURLReset:
		return errReset
	default:
		return errUnavailable
	}
}

// Resolved is the data plane's prepared form of one workspace, built once per cache fill by
// Resolve. Tenant (its Attrs map and Policy) is shared by every call of the workspace: treat it as
// read-only.
type Resolved struct {
	Tenant   tollgate.Tenant
	prefixes []netip.Prefix
	restrict bool
}

// Resolve builds the Resolved view of a workspace. tenancy.Directory.SetView takes it.
func Resolve(ws contract.Workspace) any {
	attrs := map[string]string{AttrOrgID: ws.OrgID, AttrPublicID: ws.PublicID}
	if ws.Retired {
		attrs[MetaURLRetired] = "true"
	}
	if canonicalUUID(ws.ID) && canonicalUUID(ws.OrgID) {
		attrs[AttrTenantOK] = "true"
	}
	if n := MaxConcurrentOf(ws.Settings); n > 0 {
		attrs[AttrMaxConcurrent] = strconv.Itoa(n)
	}
	res := &Resolved{Tenant: tollgate.Tenant{ID: ws.ID, Attrs: attrs, Policy: PolicyOf(ws.Settings)}, restrict: len(ws.AllowedClientCIDRs) > 0}
	for _, c := range ws.AllowedClientCIDRs {
		if p, err := netip.ParsePrefix(c); err == nil {
			res.prefixes = append(res.prefixes, p)
		}
	}
	return res
}

// sourceAllowed reports whether the TCP peer is inside one of the networks; no list allows any
// peer. An address that cannot be parsed is refused when a list is set. X-Forwarded-For is not
// read: a client can write it.
func (r *Resolved) sourceAllowed(remoteAddr string) bool {
	if !r.restrict {
		return true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range r.prefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// Uncached adapts a plain lookup to Directory with no cache: every call is a miss that builds the
// view again. For tests and tools; the server uses tenancy.Directory.
func Uncached(lookup func(ctx context.Context, publicID string) (contract.Workspace, error)) Directory {
	return uncached(lookup)
}

type uncached func(ctx context.Context, publicID string) (contract.Workspace, error)

func (uncached) CachedView(string) (any, bool, error) { return nil, false, nil }

func (u uncached) LoadView(ctx context.Context, publicID string) (any, error) {
	ws, err := u(ctx, publicID)
	if err != nil {
		return nil, err
	}
	return Resolve(ws), nil
}
