package dataplane_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func cfg(t *testing.T, kv map[string]string) config.Config {
	t.Helper()
	kv[config.EnvDatabaseURL] = "postgres://u:p@127.0.0.1:5432/db"
	c, err := config.Load(func(k string) (string, bool) { v, ok := kv[k]; return v, ok })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// fakeResolver sends every /openai call to the loopback fake upstream.
type fakeResolver struct{ base *url.URL }

func (r fakeResolver) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	return tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: r.base}, strings.TrimPrefix(req.URL.Path, "/openai"), nil
}

func TestOptionsDefaultsAndPrivacy(t *testing.T) {
	o := dataplane.Options(dataplane.Deps{})
	if _, ok := o.Auth.(tollgate.NoAuth); !ok {
		t.Errorf("default auth = %T, want NoAuth (dev only, Plan 05 replaces it)", o.Auth)
	}
	if _, ok := o.Admission.(tollgate.Unlimited); !ok {
		t.Errorf("default admission = %T, want Unlimited", o.Admission)
	}
	if _, ok := o.Metrics.(tollgate.DiscardSink); !ok {
		t.Errorf("default sink = %T, want DiscardSink", o.Metrics)
	}
	if o.Limits != tollgate.DefaultLimits() {
		t.Error("limits must be tollgate's defaults")
	}
}

func TestPrivacyModeFalseIsAcceptedButWarnsThatItDoesNothing(t *testing.T) {
	var buf bytes.Buffer
	deps := dataplane.Deps{Logger: slog.New(slog.NewJSONHandler(&buf, nil))}
	p, err := dataplane.New(cfg(t, map[string]string{config.EnvPrivacyMode: "false"}), deps)
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close(context.Background())
	if !strings.Contains(buf.String(), dataplane.PrivacyModeIgnoredWarning) || !strings.Contains(buf.String(), `"level":"WARN"`) {
		t.Fatalf("no boot warning for PRIVACY_MODE=false: %q", buf.String())
	}
	buf.Reset()
	p, err = dataplane.New(cfg(t, map[string]string{}), deps)
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close(context.Background())
	if strings.Contains(buf.String(), "PRIVACY_MODE") {
		t.Fatalf("default config must not warn: %q", buf.String())
	}
}

type spyHook struct {
	tollgate.NoopHook
	before chan string
}

func (h *spyHook) Name() string { return "spy" }
func (h *spyHook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	h.before <- c.Model
	return nil, nil
}

func TestDepsReplaceFieldsWithoutRewiring(t *testing.T) {
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	hook := &spyHook{before: make(chan string, 1)}
	p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{Resolver: fakeResolver{base}, Hooks: []tollgate.Hook{hook}})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	_, body := testkit.RequestFor("openai/chat")
	resp := post(t, srv.URL+"/openai/v1/chat/completions", body, "openai/chat")
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	select {
	case <-hook.before:
	case <-time.After(2 * time.Second):
		t.Fatal("hook from Deps.Hooks never ran")
	}
}

func post(t *testing.T, url string, body []byte, fixture string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-test-not-real")
	req.Header.Set(testkit.FixtureHeader, fixture)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestStreamPassesThroughByteForByte(t *testing.T) {
	const fixture = "openai/chat-stream-text"
	delay := 40 * time.Millisecond
	up := testkit.NewFakeUpstream(t, testkit.Options{ChunkDelay: delay})
	base, _ := url.Parse(up.URL)
	p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{Resolver: fakeResolver{base}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(ctx) }()
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	_, reqBody := testkit.RequestFor(fixture)
	start := time.Now()
	resp := post(t, srv.URL+"/openai/v1/chat/completions", reqBody, fixture)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	first := make([]byte, 1)
	if _, err := io.ReadFull(resp.Body, first); err != nil {
		t.Fatal(err)
	}
	firstAt := time.Since(start)
	rest, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	total := time.Since(start)
	want, err := testkit.Fixture(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := append(first, rest...); !bytes.Equal(got, want) {
		t.Fatalf("bytes differ: got %d, want %d", len(got), len(want))
	}
	if n := len(testkit.Chunks(fixture)); n > 2 && firstAt > total-delay {
		t.Fatalf("not streamed: first byte at %v, finished at %v over %d chunks", firstAt, total, n)
	}
	reqs := up.Requests()
	if len(reqs) != 1 || reqs[0].Header.Get("Authorization") != "Bearer sk-test-not-real" {
		t.Fatalf("the caller's own provider key must reach the upstream untouched: %+v", reqs)
	}
}

func TestHealthReadyAndAdminAnswer(t *testing.T) {
	p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close(context.Background()) }()
	proxy := httptest.NewServer(p.Handler())
	defer proxy.Close()
	admin := httptest.NewServer(p.AdminHandler())
	defer admin.Close()
	for _, u := range []string{proxy.URL + "/healthz", proxy.URL + "/readyz", admin.URL + "/statusz", admin.URL + "/metrics"} {
		if code := status(t, u); code != http.StatusOK {
			t.Errorf("%s: %d", u, code)
		}
	}
	if code := status(t, proxy.URL+"/statusz"); code == http.StatusOK {
		t.Error("admin endpoints must not be on the proxy handler")
	}
}

func status(t *testing.T, u string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}
