package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/ethosagent/meterwise/internal/contract"
)

// HeaderRequestID carries the request id in and out.
const HeaderRequestID = "X-Request-Id"

type requestIDKey struct{}

// RequestIDFrom returns the id of the current request ("" outside the chain).
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

var validRequestID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// requestID honours a well-formed inbound id (so a front proxy's id is kept) and
// otherwise generates one; it is echoed on the response.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if !validRequestID.MatchString(id) {
			var b [8]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// statusWriter records the status and size while staying transparent to
// http.ResponseController (Flush and friends).
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (s *statusWriter) code() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}

// logging writes one structured line per request: id, method, path, status,
// size and duration. It never logs headers, query strings or bodies.
func logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw, ok := w.(*statusWriter)
		if !ok {
			sw = &statusWriter{ResponseWriter: w}
		}
		next.ServeHTTP(sw, r)
		log.LogAttrs(r.Context(), slog.LevelInfo, "request",
			slog.String("request_id", RequestIDFrom(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", sw.code()),
			slog.Int("bytes", sw.bytes),
			slog.Float64("duration_ms", float64(time.Since(start).Microseconds())/1000),
		)
	})
}

// recoverer turns a handler panic into a 500 internal body and a log line.
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer recoverRequest(log, w, r)
		next.ServeHTTP(w, r)
	})
}

// recoverRequest must be deferred directly: recover only works there.
func recoverRequest(log *slog.Logger, w http.ResponseWriter, r *http.Request) {
	rec := recover()
	if rec == nil {
		return
	}
	if err, isErr := rec.(error); isErr && errors.Is(err, http.ErrAbortHandler) {
		panic(rec)
	}
	log.LogAttrs(r.Context(), slog.LevelError, "panic in handler",
		slog.String("request_id", RequestIDFrom(r.Context())), slog.Any("panic", rec))
	writeError(w, contract.NewError(contract.CodeInternal, "internal error"))
}

// tracing starts a server span per request on tp, continuing an inbound W3C
// trace context. With the default no-op provider it costs nothing.
func tracing(tp trace.TracerProvider, next http.Handler) http.Handler {
	tracer := tp.Tracer("meterwised/api")
	prop := propagation.TraceContext{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path, trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", r.Method),
				attribute.String("url.path", r.URL.Path),
				attribute.String("request.id", RequestIDFrom(r.Context())),
			))
		defer span.End()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(ctx))
		span.SetAttributes(attribute.Int("http.response.status_code", sw.code()))
		if sw.code() >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(sw.code()))
		}
	})
}

// secureHeaders sets the cheap response headers every reply should carry.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}
