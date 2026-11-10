package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"

	"github.com/ethosagent/meterwise/internal/contract"
)

// routePair is the Req and Resp types a route was registered with, recorded at boot.
type routePair struct {
	method, pattern string
	req, resp       reflect.Type
}

// none is the request type of a route without a body and the response type of one that answers
// 204.
type none struct{}

// Call is what the adapter hands a handler: the request context, the session the gateway attached,
// the principal (the zero value unless the session holds an active membership) and the URL's
// path values. Handlers never see a ResponseWriter and never look at a role.
type Call struct {
	Ctx       context.Context
	Session   contract.Session
	Principal contract.Principal
	IP        string // the TCP peer's address (no X-Forwarded-For: a client could write that), for the audit log
	r         *http.Request
}

// Path returns the value of a path parameter of the route pattern.
func (c Call) Path(name string) string { return c.r.PathValue(name) }

// Query returns the first value of a query parameter ("" when absent).
func (c Call) Query(name string) string { return c.r.URL.Query().Get(name) }

// peerIP is the host part of the request's remote address.
func peerIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	return host
}

// register adds a JSON route: the adapter owns principal extraction, body decoding (422
// validation_failed when malformed; skipped for Req = none), the one error mapping (apiError), the
// success status (from the contract row; 204 sends no body) and encoding. The row must exist and
// not be public; its Permission is enforced by the gateway before fn runs.
func register[Req, Resp any](s *server, method, pattern string, fn func(Call, Req) (Resp, error)) {
	row, ok := contract.PermissionFor(method, pattern)
	if !ok {
		s.problems = append(s.problems, errNoRow(method, pattern))
		return
	}
	s.pairs = append(s.pairs, routePair{method, pattern, reflect.TypeFor[Req](), reflect.TypeFor[Resp]()})
	status := http.StatusOK
	if row.Status != 0 {
		status = row.Status
	}
	s.handle(method, pattern, adaptAudited(s, status, row.Audit, fn))
}

// committedError marks a failure that came after the change committed (building the response). It
// carries what the committed entity yields for the audit extractors, so the one emit in
// adaptAudited still writes the row; the client gets err.
type committedError struct {
	resp any
	err  error
}

func (e *committedError) Error() string { return e.err.Error() }
func (e *committedError) Unwrap() error { return e.err }

// committed wraps a post-commit failure; with a nil err it is the identity. resp is the minimal
// response the route's audit extractors read from the committed entity.
func committed[Resp any](resp Resp, err error) (Resp, error) {
	if err == nil {
		return resp, nil
	}
	return resp, &committedError{resp: resp, err: err}
}

// adapt turns fn into an http.HandlerFunc. A request that reaches it without a session (the
// gateway did not run) is refused as unauthenticated rather than served as an empty user.
func adapt[Req, Resp any](s *server, status int, fn func(Call, Req) (Resp, error)) http.HandlerFunc {
	return adaptAudited(s, status, nil, fn)
}

// adaptAudited is adapt plus the route row's audit spec: after fn returns a nil error the one
// emit below records the event. This is the only place an audit row is written for a route.
func adaptAudited[Req, Resp any](s *server, status int, spec *contract.AuditSpec, fn func(Call, Req) (Resp, error)) http.HandlerFunc {
	_, bodyless := any(*new(Req)).(none)
	return func(w http.ResponseWriter, r *http.Request) {
		sess, ok := SessionFrom(r.Context())
		if !ok {
			writeError(w, contract.NewError(contract.CodeUnauthenticated, "authentication required"))
			return
		}
		c := Call{Ctx: r.Context(), Session: sess, IP: peerIP(r), r: r}
		if sess.Member {
			c.Principal = sess.Principal
		}
		var in Req
		if !bodyless && !readJSON(w, r, &in) {
			return
		}
		out, err := fn(c, in)
		if ce := (*committedError)(nil); errors.As(err, &ce) {
			// the change committed and only the response failed: the audit row still goes out
			if spec != nil {
				s.emitAudit(c, spec, in, ce.resp)
			}
			err = ce.err
		}
		if err != nil {
			s.fail(w, r, err)
			return
		}
		if spec != nil {
			s.emitAudit(c, spec, in, out)
		}
		if status == http.StatusNoContent {
			w.WriteHeader(status)
			return
		}
		writeJSON(w, status, out)
	}
}
