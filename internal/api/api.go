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
	Prefs           PreferenceStore
	Web             fs.FS // built web app; nil serves a typed 404 at "/"
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
	if d.Log == nil || d.DB == nil || d.Principals == nil || d.Prefs == nil {
		return nil, errors.New("api: Log, DB, Principals and Prefs are required")
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
		handler = withPrincipal(s.Principals, handler)
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
	p, _ := PrincipalFrom(r.Context())
	prefs, err := s.Prefs.Get(r.Context(), p.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

func (s *server) patchPreferences(w http.ResponseWriter, r *http.Request) {
	p, _ := PrincipalFrom(r.Context())
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
	prefs, err := s.Prefs.Update(r.Context(), p.UserID, patch)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, prefs)
}

// fail logs an unexpected error with its request id and answers a typed 500.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.LogAttrs(r.Context(), slog.LevelError, "handler error",
		slog.String("request_id", RequestIDFrom(r.Context())), slog.String("err", err.Error()))
	writeError(w, contract.AsError(err))
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
