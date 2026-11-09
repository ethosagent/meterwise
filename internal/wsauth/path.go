package wsauth

import (
	"context"
	"net/http"
	"strings"

	"github.com/ethosagent/meterwise/internal/contract"
)

const prefix = "/w/"

// Parse splits /w/{publicId}[/rest] into the id and the rest of the path ("/" when there is none).
// ok is false when the path is not under /w/ or the id does not have the shape of a public id
// (contract.ValidPublicID); the id is checked here so a junk id never reaches the directory.
func Parse(path string) (id, rest string, ok bool) {
	tail, found := strings.CutPrefix(path, prefix)
	if !found {
		return "", "", false
	}
	id, _, _ = strings.Cut(tail, "/")
	if !contract.ValidPublicID(id) {
		return "", "", false
	}
	if rest = path[len(prefix)+len(id):]; rest == "" {
		rest = "/"
	}
	return id, rest, true
}

type idKey struct{}

// WithPublicID carries the workspace id beside a request whose path no longer has it: Front strips
// /w/{id} from the path (so tollgate shapes every refusal for the provider the path names) and
// hands the id to the authenticator here.
func WithPublicID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, idKey{}, id)
}

// requestID is the public id of the request: the one Front stashed, else the one in the path.
func requestID(ctx context.Context, r *http.Request) (string, bool) {
	if id, ok := ctx.Value(idKey{}).(string); ok && contract.ValidPublicID(id) {
		return id, true
	}
	id, _, ok := Parse(r.URL.Path)
	return id, ok
}

// Front wraps the proxy handler. A request for /w/{id}/rest is passed on as /rest with the id in
// its context: the proxy then picks the error shape (OpenAI, Anthropic) from the provider prefix,
// so a 404 or 410 for a bad workspace URL reads like the provider's own error, and the id never
// appears in the path the proxy logs. Anything else is passed on untouched, and the authenticator
// answers it.
func Front(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, rest, ok := Parse(r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		u := *r.URL
		u.Path, u.RawPath = rest, ""
		r2 := r.WithContext(WithPublicID(r.Context(), id)) // shallow copy: headers and body are shared
		r2.URL = &u
		next.ServeHTTP(w, r2)
	})
}
