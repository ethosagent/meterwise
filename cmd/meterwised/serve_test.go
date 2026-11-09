package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store/pgtest"
)

// fakeResolver sends /openai calls to the loopback fake upstream.
type fakeResolver struct{ base *url.URL }

func (r fakeResolver) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	return tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: r.base}, strings.TrimPrefix(req.URL.Path, "/openai"), nil
}

// collector is a loopback OTLP/HTTP endpoint that keeps what it receives.
type collector struct {
	srv  *httptest.Server
	mu   sync.Mutex
	body [][]byte
	path []string
}

func newCollector(t *testing.T) *collector {
	t.Helper()
	c := &collector{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.body, c.path = append(c.body, b), append(c.path, r.URL.Path)
		c.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *collector) seen(needle string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, b := range c.body {
		if c.path[i] == "/v1/traces" && bytes.Contains(b, []byte(needle)) {
			return true
		}
	}
	return false
}

// startServe runs `serve -json` in the background and returns its addresses and a stop function.
func startServe(t *testing.T, kv map[string]string, deps dataplane.Deps) (map[string]string, func() int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out := &syncBuf{}
	done := make(chan int, 1)
	go func() {
		e := &env{lookup: lookup(kv), stdout: out, stderr: &syncBuf{}, deps: deps}
		done <- cmdServe(ctx, e, []string{"-json"})
	}()
	var addrs map[string]string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && addrs == nil; time.Sleep(20 * time.Millisecond) {
		var r struct {
			OK   bool              `json:"ok"`
			Data map[string]string `json:"data"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(out.String())), &r) == nil && r.OK {
			addrs = r.Data
		}
		select {
		case code := <-done:
			t.Fatalf("serve exited early with %d: %s", code, out.String())
		default:
		}
	}
	if addrs == nil {
		cancel()
		t.Fatalf("serve never reported its addresses: %q", out.String())
	}
	stop := func() int {
		cancel()
		select {
		case code := <-done:
			return code
		case <-time.After(15 * time.Second):
			t.Fatal("no graceful shutdown")
			return -1
		}
	}
	t.Cleanup(func() { cancel() })
	return addrs, stop
}

func get(t *testing.T, u string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func baseEnv(t *testing.T, dsn string) map[string]string {
	t.Helper()
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>shell</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"DATABASE_URL": dsn, "LISTEN_PROXY": "127.0.0.1:0", "LISTEN_API": "127.0.0.1:0", "LISTEN_ADMIN": "127.0.0.1:0",
		"APP_NAME": "Acme Meter", "PROXY_PUBLIC_HOST": "proxy.example.com", "WEB_DIR": web,
	}
}

func TestServeBootsAllThreeListenersAndStreamsThroughTollgate(t *testing.T) {
	dsn := pgtest.NewDatabase(t)
	col := newCollector(t)
	const fixture = "openai/chat-stream-text"
	up := testkit.NewFakeUpstream(t, testkit.Options{ChunkDelay: 30 * time.Millisecond})
	base, _ := url.Parse(up.URL)
	kv := baseEnv(t, dsn)
	kv["OTEL_EXPORTER_OTLP_ENDPOINT"] = col.srv.URL

	addrs, stop := startServe(t, kv, dataplane.Deps{Resolver: fakeResolver{base}})
	proxy, api, admin := "http://"+addrs["proxy"], "http://"+addrs["api"], "http://"+addrs["admin"]

	for _, u := range []string{proxy + "/healthz", proxy + "/readyz", admin + "/statusz", admin + "/metrics", api + "/healthz"} {
		if code, body := get(t, u); code != 200 {
			t.Errorf("%s: %d %s", u, code, body)
		}
	}
	if code, body := get(t, api+"/api/config"); code != 200 || strings.TrimSpace(body) != `{"appName":"Acme Meter","proxyPublicHost":"proxy.example.com"}` {
		t.Fatalf("/api/config: %d %s", code, body)
	}
	if code, body := get(t, api+"/some/client/route"); code != 200 || !strings.Contains(body, "shell") {
		t.Fatalf("static fallback: %d %s", code, body)
	}
	if code, _ := get(t, proxy+"/api/config"); code == 200 {
		t.Fatal("the control plane must not be on the proxy listener")
	}

	_, reqBody := testkit.RequestFor(fixture)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, proxy+"/openai/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-caller-own-key")
	req.Header.Set(testkit.FixtureHeader, fixture)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	want, _ := testkit.Fixture(fixture)
	if resp.StatusCode != 200 || !bytes.Equal(got, want) {
		t.Fatalf("proxied stream: status %d, %d bytes, want %d identical bytes", resp.StatusCode, len(got), len(want))
	}

	if code := stop(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	// shutdown flushed the trace batch to the collector
	if !col.seen("meterwised") || !col.seen("GET /api/config") {
		t.Fatal("the collector never received the control-plane trace over OTLP")
	}
	if _, err := net.DialTimeout("tcp", addrs["api"], 500*time.Millisecond); err == nil {
		t.Fatal("API listener still open after shutdown")
	}
}

func TestServeStartsWithoutTelemetryOrWebBuild(t *testing.T) {
	kv := baseEnv(t, pgtest.NewDatabase(t))
	kv["WEB_DIR"] = filepath.Join(t.TempDir(), "missing")
	addrs, stop := startServe(t, kv, dataplane.Deps{})
	code, body := get(t, "http://"+addrs["api"]+"/")
	var e contract.ErrorBody
	if code != 404 || json.Unmarshal([]byte(body), &e) != nil || e.Code != contract.CodeNotFound {
		t.Fatalf("no web build: %d %s", code, body)
	}
	if stop() != 0 {
		t.Fatal("unclean exit")
	}
}

func TestServeFailsTypedWhenTheDatabaseIsDown(t *testing.T) {
	kv := baseEnv(t, "postgres://u:topsecret@127.0.0.1:1/db?sslmode=disable")
	code, out, errb := exec(t, kv, "serve", "-json")
	r := decode(t, out)
	if code != 1 || r.Error.Code != contract.CodeUnavailable {
		t.Fatalf("%d %s", code, out)
	}
	if strings.Contains(out+errb, "topsecret") {
		t.Fatal("the password leaked")
	}
}

func TestServeReportsEveryListenerThatCannotBind(t *testing.T) {
	taken := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		return ln.Addr().String()
	}
	kv := baseEnv(t, pgtest.NewDatabase(t))
	kv["LISTEN_PROXY"], kv["LISTEN_API"] = taken(), taken()
	code, out, _ := exec(t, kv, "serve", "-json")
	r := decode(t, out)
	if code != 1 || len(r.Error.Details) != 2 || !strings.HasPrefix(r.Error.Details[0], "LISTEN_PROXY") || !strings.HasPrefix(r.Error.Details[1], "LISTEN_API") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestServeRejectsBadConfig(t *testing.T) {
	if code, out, _ := exec(t, nil, "serve", "-json"); code != 1 || decode(t, out).Error.Code != contract.CodeConfigInvalid {
		t.Fatalf("missing config: %d %q", code, out)
	}
}

func TestMigrateAppliesTheBaselineThenReportsNothingToDo(t *testing.T) {
	kv := map[string]string{"DATABASE_URL": pgtest.NewDatabase(t)}
	code, out, _ := exec(t, kv, "migrate", "status", "-json")
	if code != 0 || !strings.Contains(out, `"applied":false`) {
		t.Fatalf("status before: %d %s", code, out)
	}
	code, out, _ = exec(t, kv, "migrate", "-json")
	if code != 0 || !strings.Contains(out, "0001_store.sql") || !strings.Contains(out, `"applied":true`) {
		t.Fatalf("up: %d %s", code, out)
	}
	if code, out, _ = exec(t, kv, "migrate"); code != 0 || !strings.Contains(out, "nothing to do") {
		t.Fatalf("second up: %d %q", code, out)
	}
	if code, out, _ = exec(t, kv, "migrate", "status"); code != 0 || !strings.Contains(out, "applied") {
		t.Fatalf("status text: %d %q", code, out)
	}
}
