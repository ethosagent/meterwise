package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

const (
	pingTimeout  = 2 * time.Second
	maxBodyBytes = 64 << 10
)

// Pinger reports whether the database answers.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps is everything the control plane needs; cmd/meterwised wires it.
type Deps struct {
	Log             *slog.Logger
	DB              Pinger
	Principals      PrincipalResolver
	Tenancy         Tenancy
	Webhooks        WebhookVerifier // nil answers the Clerk webhook with not_available
	Web             fs.FS           // built web app; nil serves a typed 404 at "/"
	Tracer          trace.TracerProvider
	AppName         string
	ProxyPublicHost string
	Version         contract.Version
}

type server struct {
	Deps
	mux        *http.ServeMux
	registered map[string]bool
	problems   []error
}

// New builds the control-plane handler. The chain is request id, logging,
// recover, OTel; the principal middleware wraps each non-public route. It
// fails when the handlers and contract.Routes disagree in either direction.
func New(d Deps) (http.Handler, error) {
	if d.Log == nil || d.DB == nil || d.Principals == nil || d.Tenancy == nil {
		return nil, errors.New("api: Log, DB, Principals and Tenancy are required")
	}
	if d.Tracer == nil {
		d.Tracer = otel.GetTracerProvider()
	}
	s := &server{Deps: d, mux: http.NewServeMux(), registered: map[string]bool{}}
	s.handle("GET", "/healthz", s.health)
	s.handle("GET", "/api/v1/version", s.version)
	s.handle("GET", "/api/config", s.appConfig)
	s.handle("GET", "/api/me/preferences", s.getPreferences)
	s.handle("PATCH", "/api/me/preferences", s.patchPreferences)
	s.handle("GET", "/api/me", s.getMe)
	s.handle("POST", "/api/auth/complete-signup", s.completeSignup)
	s.handle("GET", "/api/workspaces", s.listWorkspaces)
	s.handle("POST", "/api/workspaces", s.createWorkspace)
	s.handle("PATCH", "/api/workspaces/{id}", s.updateWorkspace)
	s.handle("DELETE", "/api/workspaces/{id}", s.deleteWorkspace)
	s.handle("POST", "/api/workspaces/{id}/switch", s.switchWorkspace)
	s.handle("GET", "/api/invitations", s.listInvitations)
	s.handle("POST", "/api/invitations", s.createInvitation)
	s.handle("POST", "/api/invitations/{id}/resend", s.resendInvitation)
	s.handle("DELETE", "/api/invitations/{id}", s.revokeInvitation)
	s.handle("POST", "/api/webhooks/clerk", s.clerkWebhook)
	for _, r := range contract.Routes() {
		if !s.registered[r.Method+" "+r.Pattern] {
			s.problems = append(s.problems, fmt.Errorf("api: route %s %s is in the contract table but has no handler", r.Method, r.Pattern))
		}
	}
	if len(s.problems) > 0 {
		return nil, errors.Join(s.problems...)
	}
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, contract.NewError(contract.CodeNotFound, "no such route"))
	})
	if d.Web != nil {
		s.mux.Handle("/", staticHandler(d.Web))
	} else {
		s.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			writeError(w, contract.NewError(contract.CodeNotFound, "no such route"))
		})
	}
	var h http.Handler = s.mux
	h = tracing(d.Tracer, h)
	h = recoverer(d.Log, h)
	h = logging(d.Log, h)
	h = requestID(h)
	return secureHeaders(h), nil
}

// handle registers a route that must have a row in the contract table; a
// non-public row gets the principal middleware.
func (s *server) handle(method, pattern string, h http.HandlerFunc) {
	key := method + " " + pattern
	row, ok := contract.PermissionFor(method, pattern)
	if !ok {
		s.problems = append(s.problems, fmt.Errorf("api: handler %s has no row in the contract route table", key))
		return
	}
	var handler http.Handler = h
	if !row.Public {
		handler = s.gateway(s.Principals, row.Permission, handler)
	}
	s.registered[key] = true
	s.mux.Handle(key, handler)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), pingTimeout)
	defer cancel()
	if err := s.DB.Ping(ctx); err != nil {
		s.Log.LogAttrs(r.Context(), slog.LevelError, "health: database ping failed", slog.String("err", err.Error()))
		writeError(w, contract.NewError(contract.CodeUnavailable, "database unavailable"))
		return
	}
	writeJSON(w, http.StatusOK, contract.Health{Status: "ok"})
}

func (s *server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Version)
}

func (s *server) appConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, contract.AppConfig{AppName: s.AppName, ProxyPublicHost: s.ProxyPublicHost})
}

func (s *server) getPreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r.Context())
	if !ok {
		writeError(w, contract.NewError(contract.CodeUnauthenticated, "authentication required"))
		return
	}
	prefs, err := s.Tenancy.Preferences(r.Context(), sess.User.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *server) patchPreferences(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r.Context())
	if !ok {
		writeError(w, contract.NewError(contract.CodeUnauthenticated, "authentication required"))
		return
	}
	var patch contract.PreferencesPatch
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&patch); err != nil {
		writeError(w, contract.NewError(contract.CodeInvalidRequest, "body must be a JSON object with theme and/or reducedMotion", "body: "+decodeProblem(err)))
		return
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writeError(w, contract.NewError(contract.CodeInvalidRequest, "body must be a single JSON object"))
		return
	}
	if patch.Theme != nil && !patch.Theme.Valid() {
		writeError(w, contract.NewError(contract.CodeInvalidRequest, "theme is not valid", "theme: must be one of light|dark|system"))
		return
	}
	prefs, err := s.Tenancy.UpdatePreferences(r.Context(), sess.User.ID, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// apiError is the one mapping from an error to what a client sees: a typed
// *contract.Error as is, an unreachable database as 503 unavailable (retry),
// any other store failure or unknown error as 500 internal. Nothing else may
// decide a status, so the gateway and the handlers cannot disagree.
func apiError(err error) *contract.Error {
	var typed *contract.Error
	if errors.As(err, &typed) {
		return typed
	}
	if store.ClassOf(err) == store.ClassUnreachable {
		return contract.NewError(contract.CodeUnavailable, "a dependency is not reachable right now; retry")
	}
	return contract.AsError(err)
}

// fail answers the typed error for err. Only a 5xx is logged (with its request id):
// a 4xx is the caller's mistake and already carries its code.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	e := apiError(err)
	if r.Context().Err() != nil {
		// The client went away mid-request (a closed tab); the driver reports that as a failed
		// query. Nobody is waiting for the answer and nothing is wrong with the service.
		s.Log.LogAttrs(r.Context(), slog.LevelDebug, "request abandoned",
			slog.String("request_id", RequestIDFrom(r.Context())))
	} else if e.Status() >= http.StatusInternalServerError {
		s.Log.LogAttrs(r.Context(), slog.LevelError, "handler error",
			slog.String("request_id", RequestIDFrom(r.Context())), slog.String("err", err.Error()))
	}
	writeError(w, e)
}

// decodeProblem names what is wrong with a body without echoing its content.
func decodeProblem(err error) string {
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	var tooBig *http.MaxBytesError
	switch {
	case errors.Is(err, io.EOF):
		return "empty"
	case errors.As(err, &syn):
		return "not valid JSON"
	case errors.As(err, &typ):
		return "field " + typ.Field + " has the wrong type"
	case errors.As(err, &tooBig):
		return "too large"
	default:
		return "unknown or malformed field"
	}
}
