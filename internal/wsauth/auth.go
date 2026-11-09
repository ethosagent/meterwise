package wsauth

import (
	"context"
	"net"
	"net/http"
	"net/netip"
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
	AttrOrgID    = "org_id"
	AttrPublicID = "public_id"
)

// Directory is the lookup the authenticator needs; contract.WorkspaceDirectory implements it.
type Directory interface {
	ByPublicID(ctx context.Context, publicID string) (contract.Workspace, error)
}

// Error is an authentication failure that picks the HTTP status. tollgate maps the status and
// writes a provider-shaped body whose text depends on the status alone (ADR 0008), so the text
// here is for logs and tests: a 410 reads as "gone" to the client, and that is the signal for a
// reset URL.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string   { return e.Msg }
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
}

var _ tollgate.Authenticator = (*Authenticator)(nil)

// New builds the authenticator; timeout 0 means LookupTimeout.
func New(dir Directory, timeout time.Duration) *Authenticator {
	if timeout <= 0 {
		timeout = LookupTimeout
	}
	return &Authenticator{dir: dir, timeout: timeout}
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
func (a *Authenticator) Authenticate(ctx context.Context, r *http.Request) (tollgate.Tenant, error) {
	id, _, ok := Parse(r.URL.Path)
	if !ok {
		return tollgate.Tenant{}, errUnknown
	}
	lctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	ws, err := a.dir.ByPublicID(lctx, id)
	if err != nil {
		switch contract.AsError(err).Code {
		case contract.CodeNotFound:
			return tollgate.Tenant{}, errUnknown
		case contract.CodeWorkspaceURLReset:
			return tollgate.Tenant{}, errReset
		default:
			return tollgate.Tenant{}, errUnavailable
		}
	}
	if !sourceAllowed(r.RemoteAddr, ws.AllowedClientCIDRs) {
		return tollgate.Tenant{}, errSourceIP
	}
	attrs := map[string]string{AttrOrgID: ws.OrgID, AttrPublicID: ws.PublicID}
	if ws.Retired {
		attrs[MetaURLRetired] = "true"
	}
	return tollgate.Tenant{ID: ws.ID, Attrs: attrs, Policy: PolicyOf(ws.Settings)}, nil
}

// sourceAllowed reports whether the TCP peer is inside one of the networks; an empty list allows
// any peer. An address that cannot be parsed is refused when a list is set. X-Forwarded-For is not
// read: a client can write it.
func sourceAllowed(remoteAddr string, cidrs []string) bool {
	if len(cidrs) == 0 {
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
	for _, c := range cidrs {
		if p, err := netip.ParsePrefix(c); err == nil && p.Contains(ip) {
			return true
		}
	}
	return false
}
