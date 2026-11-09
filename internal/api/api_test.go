package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/store"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

type fixture struct {
	srv   *httptest.Server
	res   *countingResolver
	logs  *bytes.Buffer
	spans *tracetest.InMemoryExporter
}

func newFixture(t *testing.T, mutate func(*api.Deps)) *fixture {
	t.Helper()
	f := &fixture{res: &countingResolver{}, logs: &bytes.Buffer{}, spans: tracetest.NewInMemoryExporter()}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(f.spans))
	d := api.Deps{
		Log:        slog.New(slog.NewJSONHandler(f.logs, nil)),
		DB:         pinger{},
		Principals: f.res,
		Tenancy:    newMemTenancy(),
		Authz:      allowAuthz{},
		Access:     noAccess{},
		Audit:      &memAudit{},
		Web: fstest.MapFS{
			"index.html":          {Data: []byte("<html>app</html>")},
			"assets/app-1.js":     {Data: []byte("console.log(1)")},
			"assets/app-1.js.map": {Data: []byte(`{"sources":["src/secret.ts"]}`)},
		},
		Tracer:          tp,
		AppName:         "Acme Meter",
		ProxyPublicHost: "proxy.example.com",
		Version:         contract.Version{Version: "1.2.3", Commit: "abc"},
	}
	if mutate != nil {
		mutate(&d)
	}
	h, err := api.New(d)
	if err != nil {
		t.Fatal(err)
	}
	f.srv = httptest.NewServer(h)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) do(t *testing.T, method, path, body string, hdr ...string) (*http.Response, []byte) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, f.srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := f.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func decodeErr(t *testing.T, b []byte) contract.ErrorBody {
	t.Helper()
	var e contract.ErrorBody
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("not an error body: %q", b)
	}
	if e.Details == nil {
		t.Fatalf("details must be an object, never absent: %q", b)
	}
	return e
}

func TestNewRequiresDependenciesAndMatchesTheRouteTable(t *testing.T) {
	if _, err := api.New(api.Deps{}); err == nil {
		t.Fatal("empty Deps accepted")
	}
	f := newFixture(t, nil)
	for _, r := range contract.Routes() {
		if r.Method != "GET" {
			continue
		}
		resp, _ := f.do(t, r.Method, r.Pattern, "")
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s %s: status %d", r.Method, r.Pattern, resp.StatusCode)
		}
	}
}

func TestAppConfigIsPublicAndCarriesTheEnvValues(t *testing.T) {
	f := newFixture(t, nil)
	f.res.err = errors.New("no session")
	resp, b := f.do(t, "GET", "/api/config", "")
	if resp.StatusCode != 200 || strings.TrimSpace(string(b)) != `{"appName":"Acme Meter","proxyPublicHost":"proxy.example.com"}` {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if f.res.calls != 0 {
		t.Fatal("a public route must not consult the principal resolver")
	}
}

func TestPreferencesLifecycle(t *testing.T) {
	f := newFixture(t, nil)
	_, b := f.do(t, "GET", "/api/me/preferences", "")
	if strings.TrimSpace(string(b)) != `{"theme":"system","reducedMotion":false}` {
		t.Fatalf("defaults: %s", b)
	}
	_, b = f.do(t, "PATCH", "/api/me/preferences", `{"theme":"dark"}`)
	if strings.TrimSpace(string(b)) != `{"theme":"dark","reducedMotion":false}` {
		t.Fatalf("theme patch: %s", b)
	}
	_, b = f.do(t, "PATCH", "/api/me/preferences", `{"reducedMotion":true}`)
	if strings.TrimSpace(string(b)) != `{"theme":"dark","reducedMotion":true}` {
		t.Fatalf("partial patch must keep the theme: %s", b)
	}
	_, b = f.do(t, "PATCH", "/api/me/preferences", `{}`)
	if strings.TrimSpace(string(b)) != `{"theme":"dark","reducedMotion":true}` {
		t.Fatalf("empty patch: %s", b)
	}
	_, b = f.do(t, "GET", "/api/me/preferences", "")
	if strings.TrimSpace(string(b)) != `{"theme":"dark","reducedMotion":true}` {
		t.Fatalf("persisted: %s", b)
	}
}

func TestPreferencesRejectBadBodiesWithTypedErrors(t *testing.T) {
	f := newFixture(t, nil)
	// A bad theme value is a well-formed body with a bad value (400); every malformed body
	// is validation_failed 422 like on every other route.
	if resp, b := f.do(t, "PATCH", "/api/me/preferences", `{"theme":"neon"}`); resp.StatusCode != http.StatusBadRequest || decodeErr(t, b).Code != contract.CodeInvalidRequest {
		t.Fatalf("bad theme: %d %s", resp.StatusCode, b)
	}
	tests := map[string]string{
		"unknown field": `{"colour":"red"}`,
		"wrong type":    `{"reducedMotion":"yes"}`,
		"not json":      `theme=dark`,
		"empty":         ``,
		"two documents": `{"theme":"dark"}{"theme":"light"}`,
		"too large":     `{"theme":"` + strings.Repeat("a", 70_000) + `"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			resp, b := f.do(t, "PATCH", "/api/me/preferences", body)
			e := decodeErr(t, b)
			if resp.StatusCode != http.StatusUnprocessableEntity || e.Code != contract.CodeValidationFailed {
				t.Fatalf("%d %s", resp.StatusCode, b)
			}
		})
	}
	_, b := f.do(t, "GET", "/api/me/preferences", "")
	if strings.TrimSpace(string(b)) != `{"theme":"system","reducedMotion":false}` {
		t.Fatalf("a rejected patch changed state: %s", b)
	}
}

func TestUnauthenticatedGets401WithTheRegistryStatus(t *testing.T) {
	f := newFixture(t, nil)
	f.res.err = contract.NewError(contract.CodeUnauthenticated, "invalid or expired token")
	for _, m := range []string{"GET", "PATCH"} {
		resp, b := f.do(t, m, "/api/me/preferences", `{}`)
		e := decodeErr(t, b)
		if resp.StatusCode != 401 || e.Code != contract.CodeUnauthenticated {
			t.Errorf("%s: %d %s", m, resp.StatusCode, b)
		}
	}
}

// One mapper decides every status: an unexpected Resolve error is a logged 500 (never a silent
// 401), an unreachable database is 503, and the same store error maps the same way from a handler.
func TestResolveFailuresShareTheHandlersErrorMapper(t *testing.T) {
	unreachable := &store.Error{Class: store.ClassUnreachable, Message: "store: database unreachable"}
	queryFailed := &store.Error{Class: store.ClassPool, Message: "store: query failed (SQLSTATE 42P01)"}
	tests := []struct {
		name   string
		err    error
		status int
		code   contract.Code
		logged bool
	}{
		{"unexpected error", errors.New("boom: password=hunter2"), 500, contract.CodeInternal, true},
		{"store query failure", queryFailed, 500, contract.CodeInternal, true},
		{"database unreachable", unreachable, 503, contract.CodeUnavailable, true},
		{"identity provider unavailable", contract.NewError(contract.CodeUnavailable, "keys unavailable"), 503, contract.CodeUnavailable, true},
		{"wrapped typed 401", fmt.Errorf("verify: %w", contract.NewError(contract.CodeUnauthenticated, "invalid or expired token")), 401, contract.CodeUnauthenticated, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, nil)
			f.res.err = tc.err
			resp, b := f.do(t, "GET", "/api/me/preferences", "")
			if e := decodeErr(t, b); resp.StatusCode != tc.status || e.Code != tc.code {
				t.Fatalf("%d %s", resp.StatusCode, b)
			}
			if strings.Contains(string(b), "hunter2") {
				t.Fatalf("the error leaked into the body: %s", b)
			}
			if got := strings.Contains(f.logs.String(), `"handler error"`); got != tc.logged {
				t.Fatalf("logged=%v, want %v; logs: %s", got, tc.logged, f.logs.String())
			}
		})
	}
}

// A closed tab cancels the request context and the driver reports it as a failed query; that is not an
// ERROR in the service's log.
func TestAnAbandonedRequestIsNotLoggedAsAServerError(t *testing.T) {
	f := newFixture(t, nil)
	f.res.err = &store.Error{Class: store.ClassUnreachable, Message: "store: database unreachable"}
	h, err := api.New(api.Deps{
		Log: slog.New(slog.NewJSONHandler(f.logs, nil)), DB: pinger{}, Principals: f.res, Tenancy: newMemTenancy(),
		Authz: allowAuthz{}, Access: noAccess{}, Audit: &memAudit{}, Web: fstest.MapFS{"index.html": {Data: []byte("x")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("GET", "/api/me/preferences", nil).WithContext(ctx)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if strings.Contains(f.logs.String(), `"level":"ERROR"`) {
		t.Fatalf("an abandoned request logged an error: %s", f.logs.String())
	}
}

func TestHealthzPingsTheDatabase(t *testing.T) {
	f := newFixture(t, nil)
	if resp, b := f.do(t, "GET", "/healthz", ""); resp.StatusCode != 200 || strings.TrimSpace(string(b)) != `{"status":"ok"}` {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	down := newFixture(t, func(d *api.Deps) { d.DB = pinger{err: errors.New("dial tcp: refused password=hunter2")} })
	resp, b := down.do(t, "GET", "/healthz", "")
	e := decodeErr(t, b)
	if resp.StatusCode != 503 || e.Code != contract.CodeUnavailable || strings.Contains(string(b), "hunter2") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
}

func TestUnknownAPIRouteIsJSONNotTheSPA(t *testing.T) {
	f := newFixture(t, nil)
	for _, p := range []string{"/api/nope", "/api/", "/api/me/nope"} {
		resp, b := f.do(t, "GET", p, "")
		if resp.StatusCode != 404 || decodeErr(t, b).Code != contract.CodeNotFound {
			t.Errorf("%s: %d %s", p, resp.StatusCode, b)
		}
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
			t.Errorf("%s: content type %q", p, resp.Header.Get("Content-Type"))
		}
	}
}

func TestStaticServing(t *testing.T) {
	f := newFixture(t, nil)
	resp, b := f.do(t, "GET", "/", "")
	if resp.StatusCode != 200 || !strings.Contains(string(b), "app") || resp.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("/: %d %q %q", resp.StatusCode, b, resp.Header.Get("Cache-Control"))
	}
	if resp, b = f.do(t, "GET", "/index.html", ""); resp.StatusCode != 200 || !strings.Contains(string(b), "app") {
		t.Fatalf("/index.html: %d %q", resp.StatusCode, b)
	}
	resp, b = f.do(t, "GET", "/sessions/abc", "")
	if resp.StatusCode != 200 || !strings.Contains(string(b), "app") {
		t.Fatalf("client route must fall back to index.html: %d %q", resp.StatusCode, b)
	}
	resp, b = f.do(t, "GET", "/assets/app-1.js", "")
	if resp.StatusCode != 200 || string(b) != "console.log(1)" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %q", resp.StatusCode, b, resp.Header.Get("Cache-Control"))
	}
	for _, p := range []string{"/assets/app-1.js.map", "/assets/app-1.js.MAP", "/assets//app-1.js.map"} {
		if resp, b = f.do(t, "GET", p, ""); resp.StatusCode != 404 || strings.Contains(string(b), "secret") {
			t.Fatalf("source map %s served: %d %s", p, resp.StatusCode, b)
		}
	}
	resp, b = f.do(t, "GET", "/assets/missing.js", "")
	if resp.StatusCode != 404 || decodeErr(t, b).Code != contract.CodeNotFound {
		t.Fatalf("missing asset: %d %s", resp.StatusCode, b)
	}
	resp, b = f.do(t, "GET", "/../../etc/passwd", "")
	if resp.StatusCode == 200 && strings.Contains(string(b), "root:") {
		t.Fatal("path traversal")
	}
	if resp, _ := f.do(t, "POST", "/", ""); resp.StatusCode != 404 {
		t.Fatalf("POST /: %d", resp.StatusCode)
	}
}

func TestMissingWebBuildAnswersTypedNotFound(t *testing.T) {
	for name, web := range map[string]func(*api.Deps){
		"nil fs":      func(d *api.Deps) { d.Web = nil },
		"empty build": func(d *api.Deps) { d.Web = fstest.MapFS{} },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, web)
			resp, b := f.do(t, "GET", "/", "")
			if resp.StatusCode != 404 || decodeErr(t, b).Code != contract.CodeNotFound {
				t.Fatalf("%d %s", resp.StatusCode, b)
			}
		})
	}
}

func TestRequestIDIsHonouredOrGenerated(t *testing.T) {
	f := newFixture(t, nil)
	resp, _ := f.do(t, "GET", "/api/config", "", "X-Request-Id", "edge-123.abc")
	if got := resp.Header.Get("X-Request-Id"); got != "edge-123.abc" {
		t.Fatalf("kept id = %q", got)
	}
	for _, bad := range []string{"has space", "<script>", strings.Repeat("a", 65)} {
		resp, _ = f.do(t, "GET", "/api/config", "", "X-Request-Id", bad)
		if got := resp.Header.Get("X-Request-Id"); got == bad || len(got) != 16 {
			t.Fatalf("bad id %q was kept or not replaced: %q", bad, got)
		}
	}
}

func TestLogsCarryNoCredentialsQueriesOrBodies(t *testing.T) {
	f := newFixture(t, nil)
	f.do(t, "PATCH", "/api/me/preferences?token=qsecret", `{"theme":"dark"}`, "Authorization", "Bearer sk-live-secret", "X-Api-Key", "k-secret")
	out := f.logs.String()
	for _, leak := range []string{"sk-live-secret", "k-secret", "qsecret", `"theme":"dark"`} {
		if strings.Contains(out, leak) {
			t.Fatalf("log leaks %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, `"path":"/api/me/preferences"`) || !strings.Contains(out, `"status":200`) || !strings.Contains(out, `"request_id"`) {
		t.Fatalf("log line incomplete: %s", out)
	}
}

func TestPrincipalReachesTheHandlerOnlyThroughTheResolver(t *testing.T) {
	f := newFixture(t, nil)
	f.do(t, "GET", "/api/me/preferences", "")
	if f.res.calls != 1 {
		t.Fatalf("resolver calls = %d, want exactly 1", f.res.calls)
	}
	src, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range src {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") || n == "principal.go" {
			continue
		}
		b, _ := os.ReadFile(n)
		if strings.Contains(string(b), "sessionKey") {
			t.Errorf("%s touches the principal context key; only principal.go may attach a session", n)
		}
	}
}

func TestEveryRequestStartsASpan(t *testing.T) {
	f := newFixture(t, nil)
	f.do(t, "GET", "/api/config", "", "Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	f.do(t, "GET", "/healthz", "")
	spans := f.spans.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %d, want 2", len(spans))
	}
	s := spans[0]
	if s.Name != "GET /api/config" || s.SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("span %q trace %s: inbound W3C context was not continued", s.Name, s.SpanContext.TraceID())
	}
}

// A middleware that forgot to attach the session would leave the user id empty (or, now, 401): both
// fail this test, which pins the user the handler acts for to the one the resolver returned.
func TestHandlersActForThePrincipalTheResolverReturned(t *testing.T) {
	rec := newMemTenancy()
	f := newFixture(t, func(d *api.Deps) { d.Tenancy = rec })
	if resp, b := f.do(t, "GET", "/api/me/preferences", ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("GET: %d %s", resp.StatusCode, b)
	}
	if resp, b := f.do(t, "PATCH", "/api/me/preferences", `{"theme":"dark"}`); resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH: %d %s", resp.StatusCode, b)
	}
	if len(rec.gets) != 1 || rec.gets[0] != "u-1" || len(rec.updates) != 1 || rec.updates[0] != "u-1" {
		t.Fatalf("handlers acted for gets=%v updates=%v, want u-1 on both", rec.gets, rec.updates)
	}
}
