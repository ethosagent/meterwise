package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
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

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
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

// migratedDB is a throwaway database with every migration applied: boot prepares
// the default workspace, so it needs the schema.
func migratedDB(t *testing.T) string {
	t.Helper()
	dsn := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), dsn); err != nil {
		t.Fatal(err)
	}
	return dsn
}

func baseEnv(t *testing.T, dsn string) map[string]string {
	t.Helper()
	web := t.TempDir()
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>shell</html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"DATABASE_URL": dsn, "LISTEN_PROXY": "127.0.0.1:0", "LISTEN_API": "127.0.0.1:0", "LISTEN_ADMIN": "127.0.0.1:0",
		"APP_ENV": "dev", "APP_NAME": "Acme Meter", "PROXY_PUBLIC_HOST": "proxy.example.com", "WEB_DIR": web,
	}
}

func TestServeBootsAllThreeListenersAndStreamsThroughTollgate(t *testing.T) {
	dsn := migratedDB(t)
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
	kv := baseEnv(t, migratedDB(t))
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
	for _, dsn := range []string{
		"postgres://u:topsecret@127.0.0.1:1/db?sslmode=disable",
		"postgres://u:top%73ecret%2F@127.0.0.1:1/db?sslmode=disable",
		"postgres://u@127.0.0.1:1/db?password=topsecret&sslmode=disable",
		"postgres://u@127.0.0.1:1/db?password=top%73ecret",
	} {
		kv := baseEnv(t, dsn)
		code, out, errb := exec(t, kv, "serve", "-json")
		r := decode(t, out)
		if code != 1 || r.Error.Code != contract.CodeUnavailable {
			t.Fatalf("%s: %d %s", dsn, code, out)
		}
		for _, leak := range []string{"topsecret", "top%73ecret", "topsecret%2F", "127.0.0.1"} {
			if strings.Contains(out+errb, leak) {
				t.Errorf("%s: %q leaked into the output or the log: %s | %s", dsn, leak, out, errb)
			}
		}
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
	kv := baseEnv(t, migratedDB(t))
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

func TestServeRefusesTheStubPrincipalOutsideDev(t *testing.T) {
	kv := baseEnv(t, "postgres://u:p@127.0.0.1:1/none?sslmode=disable")
	kv["APP_ENV"] = "test"
	// The guard runs before the database is opened, so an unreachable DSN proves
	// the refusal came first (otherwise the code would be "unavailable").
	code, out, _ := exec(t, kv, "serve", "-json")
	r := decode(t, out)
	if code != 1 || r.Error.Code != contract.CodeConfigInvalid || !strings.Contains(r.Error.Message, "non-stub principal resolver") {
		t.Fatalf("%d %s", code, out)
	}
	// prod without Clerk is stopped earlier still, by the config check, which names every missing variable.
	kv["APP_ENV"] = "prod"
	code, out, _ = exec(t, kv, "serve", "-json")
	r = decode(t, out)
	if code != 1 || r.Error.Code != contract.CodeConfigInvalid || len(r.Error.Details) != 5 || !strings.Contains(strings.Join(r.Error.Details, "|"), "CLERK_AUTHORIZED_PARTIES") {
		t.Fatalf("prod: %d %s", code, out)
	}
}

func TestTheStubResolverCannotBeBuiltOutsideDev(t *testing.T) {
	load := func(context.Context) (contract.Session, error) { return contract.Session{}, nil }
	if _, err := api.NewStubPrincipalResolver("dev", load); err != nil {
		t.Fatalf("dev refused: %v", err)
	}
	for _, appEnv := range []string{"prod", "test", "", "Dev", "development"} {
		if r, err := api.NewStubPrincipalResolver(appEnv, load); err == nil || r != nil {
			t.Errorf("APP_ENV=%q built the stub resolver", appEnv)
		}
	}
}

func TestRequireDevForNoAuthRefusesEveryFormOfNoAuthOutsideDev(t *testing.T) {
	var nilPtr *tollgate.NoAuth
	// a nil interface is not NoAuth any more: serve then builds the /w/{id} authenticator
	forms := map[string]tollgate.Authenticator{"value": tollgate.NoAuth{}, "pointer": &tollgate.NoAuth{}, "nil pointer": nilPtr}
	for _, appEnv := range []string{"prod", "test", ""} {
		for name, auth := range forms {
			if err := requireDevForNoAuth(appEnv, auth); err == nil || !strings.Contains(err.Message, "no authenticator") {
				t.Errorf("%s NoAuth allowed with APP_ENV=%q: %v", name, appEnv, err)
			}
		}
		if err := requireDevForNoAuth(appEnv, realAuth{}); err != nil {
			t.Errorf("a real authenticator refused with APP_ENV=%q: %v", appEnv, err)
		}
	}
	if err := requireDevForNoAuth("prod", nil); err != nil {
		t.Errorf("a nil authenticator must be allowed (serve builds the real one): %v", err)
	}
	for name, auth := range forms {
		if err := requireDevForNoAuth("dev", auth); err != nil {
			t.Errorf("dev with %s refused: %v", name, err)
		}
	}
}

// bootGuardConfig is a config whose DSN cannot connect: a guard refusal is a config_invalid
// error, and a boot that gets past both guards fails later as unavailable. That tells the two apart.
func bootGuardConfig(env string, clerk bool) config.Config {
	cfg := config.Config{Env: env, DatabaseURL: "postgres://u:p@127.0.0.1:1/none?sslmode=disable", MaxWorkspaces: 5}
	if clerk {
		cfg.Clerk = config.Clerk{Issuer: "https://clerk.example.com", JWKSURL: "https://clerk.example.com/jwks", SecretKey: "sk_x", WebhookSecret: "whsec_ZGVtbw==", APIURL: config.DefaultClerkAPIURL}
	}
	return cfg
}

// Each production guard is asserted on its own through boot: removing either call from boot fails one of these.
func TestBootGuardsAreIndependent(t *testing.T) {
	var nilPtr *tollgate.NoAuth
	tests := []struct {
		name    string
		cfg     config.Config
		auth    tollgate.Authenticator
		want    contract.Code
		message string
	}{
		{"prod, real resolver, no explicit proxy auth gets the workspace authenticator", bootGuardConfig("prod", true), nil, contract.CodeUnavailable, ""},
		{"prod, real resolver, NoAuth value", bootGuardConfig("prod", true), tollgate.NoAuth{}, contract.CodeConfigInvalid, "no authenticator"},
		{"prod, real resolver, NoAuth pointer", bootGuardConfig("prod", true), &tollgate.NoAuth{}, contract.CodeConfigInvalid, "no authenticator"},
		{"prod, real resolver, nil NoAuth pointer", bootGuardConfig("prod", true), nilPtr, contract.CodeConfigInvalid, "no authenticator"},
		{"prod, no Clerk, real proxy auth", bootGuardConfig("prod", false), realAuth{}, contract.CodeConfigInvalid, "non-stub principal resolver"},
		{"prod, no Clerk, no proxy auth", bootGuardConfig("prod", false), nil, contract.CodeConfigInvalid, "non-stub principal resolver"},
		{"prod, real resolver, real proxy auth passes both guards", bootGuardConfig("prod", true), realAuth{}, contract.CodeUnavailable, ""},
		{"dev, stub, NoAuth passes both guards", bootGuardConfig("dev", false), nil, contract.CodeUnavailable, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, cerr := boot(context.Background(), tc.cfg, slog.New(slog.NewJSONHandler(io.Discard, nil)), dataplane.Deps{Auth: tc.auth})
			if cerr == nil || cerr.Code != tc.want || !strings.Contains(cerr.Message, tc.message) {
				t.Fatalf("got %+v, want %s containing %q", cerr, tc.want, tc.message)
			}
		})
	}
}

// realAuth stands in for a real proxy authenticator.
type realAuth struct{ tollgate.Authenticator }

func TestTelemetryBootFailureNamesTheCauseWithoutCredentials(t *testing.T) {
	// config.Load rejects a malformed endpoint, so build the Config directly to reach the boot path.
	cfg := config.Config{Env: "dev", OTLPEndpoint: "http://user:topsecret@[::1/"}
	var logs bytes.Buffer
	_, cerr := boot(context.Background(), cfg, slog.New(slog.NewJSONHandler(&logs, nil)), dataplane.Deps{Auth: realAuth{}})
	if cerr == nil || cerr.Code != contract.CodeInternal || len(cerr.Details) == 0 {
		t.Fatalf("want a typed internal error with the cause in details, got %+v", cerr)
	}
	if all := cerr.Message + strings.Join(cerr.Details, " ") + logs.String(); strings.Contains(all, "topsecret") {
		t.Fatalf("credentials leaked: %s", all)
	}
}

func TestMigrateFailureIsTypedWithNoConnectionDetails(t *testing.T) {
	kv := map[string]string{"DATABASE_URL": "postgres://dbuser:dbpass@127.0.0.1:1/dbname?sslmode=disable"}
	code, out, errOut := exec(t, kv, "migrate", "-json")
	if code != 1 || decode(t, out).Error.Code != contract.CodeUnavailable {
		t.Fatalf("%d %s", code, out)
	}
	for _, leak := range []string{"dbuser", "dbpass", "127.0.0.1", "dbname"} {
		if strings.Contains(out, leak) || strings.Contains(errOut, leak) {
			t.Errorf("connection detail %q leaked: %s | %s", leak, out, errOut)
		}
	}
	if !strings.Contains(errOut, `"action":"up"`) {
		t.Errorf("cause not logged: %q", errOut)
	}
}
