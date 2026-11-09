package api

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestRecovererTurnsAPanicIntoATyped500(t *testing.T) {
	var logs bytes.Buffer
	h := recoverer(slog.New(slog.NewJSONHandler(&logs, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom with secret-token")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"code":"internal"`) || strings.Contains(rec.Body.String(), "secret-token") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(logs.String(), "panic in handler") {
		t.Fatalf("panic not logged: %s", logs.String())
	}
}

func TestRecovererRepanicsAbortHandler(t *testing.T) {
	h := recoverer(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if recover() == nil {
			t.Fatal("ErrAbortHandler must propagate")
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
}

func TestStatusWriterStaysFlushable(t *testing.T) {
	var flushed bool
	h := tracing(sdktrace.NewTracerProvider(sdktrace.WithSyncer(tracetest.NewInMemoryExporter())), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flushed = http.NewResponseController(w).Flush() == nil
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if !flushed {
		t.Fatal("wrapping the writer hid http.Flusher")
	}
}

func TestServerErrorMarksTheSpan(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	h := tracing(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp)), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	s := exp.GetSpans()
	if len(s) != 1 || s[0].Status.Code.String() != "Error" {
		t.Fatalf("spans %+v", s)
	}
}
