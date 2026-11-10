package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/store"
)

// bothWires sends /openai to the chat wire and /anthropic to the messages wire of one loopback upstream.
type bothWires struct{ base *url.URL }

func (r bothWires) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	if rest, ok := strings.CutPrefix(req.URL.Path, "/anthropic"); ok {
		return tollgate.Provider{Name: "anthropic", Wire: tollgate.WireAnthropicMessages, BaseURL: r.base}, rest, nil
	}
	return tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: r.base}, strings.TrimPrefix(req.URL.Path, "/openai"), nil
}

// plane is a booted meterwised with its three addresses and the workspace the tests use.
type plane struct {
	rt                *instance
	proxy, api, admin string
	ws                contract.Workspace
	logs              *syncBuf
	stop              func()
}

func bootPlane(t *testing.T, deps dataplane.Deps, mutate func(*config.Config)) *plane {
	t.Helper()
	cfg := config.Config{
		Env: "dev", DatabaseURL: migratedDB(t), ListenProxy: "127.0.0.1:0", ListenAPI: "127.0.0.1:0", ListenAdmin: "127.0.0.1:0",
		MaxWorkspaces: 3, WebDir: t.TempDir(), MaxConcurrentPerWorkspace: 4, PrivacyMode: true, ProxyPublicHost: "proxy.example.com", AppName: "Acme",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	p := &plane{logs: &syncBuf{}}
	ctx, cancel := context.WithCancel(context.Background())
	rt, cerr := boot(ctx, cfg, slog.New(slog.NewJSONHandler(p.logs, &slog.HandlerOptions{Level: slog.LevelDebug})), deps)
	if cerr != nil {
		cancel()
		t.Fatal(cerr)
	}
	done := make(chan error, 1)
	go func() { done <- rt.serve(ctx) }()
	p.stop = sync.OnceFunc(func() { cancel(); <-done })
	t.Cleanup(func() { p.stop(); rt.close(context.Background()) })
	p.rt = rt
	p.proxy, p.api, p.admin = "http://"+rt.lns["proxy"].Addr().String(), "http://"+rt.lns["api"].Addr().String(), "http://"+rt.lns["admin"].Addr().String()
	list, err := rt.svc.Directory().List(ctx, contract.Principal{OrgID: store.OrgID()})
	if err != nil || len(list) == 0 {
		t.Fatalf("workspaces: %v %v", list, err)
	}
	p.ws = list[0]
	return p
}

// call POSTs one fixture's request through /w/{id}; session tags the run (x-meterwise-session).
func (p *plane) call(t *testing.T, publicID, fixture, session string) (int, http.Header, []byte) {
	t.Helper()
	provider, body := testkit.RequestFor(fixture)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.proxy+"/w/"+publicID+"/"+provider+testkit.PathFor(fixture), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-caller-own-key")
	req.Header.Set(testkit.FixtureHeader, fixture)
	if session != "" {
		req.Header.Set("x-meterwise-session", session)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, b
}

func (p *plane) getJSON(t *testing.T, path string, into any) {
	t.Helper()
	code, body := get(t, p.api+path)
	if code != 200 {
		t.Fatalf("GET %s: %d %s", path, code, body)
	}
	if err := json.Unmarshal([]byte(body), into); err != nil {
		t.Fatalf("GET %s: %v\n%s", path, err, body)
	}
}

// waitCalls polls the stats route until the ledger has written n calls (the batcher is asynchronous).
func (p *plane) waitCalls(t *testing.T, n int) {
	t.Helper()
	var st struct {
		Calls int `json:"calls"`
	}
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		p.getJSON(t, "/api/workspaces/"+p.ws.ID+"/stats?range=7d", &st)
		if st.Calls >= n {
			return
		}
	}
	t.Fatalf("the ledger wrote %d of %d calls", st.Calls, n)
}

var microUSDString = regexp.MustCompile(`^\d+\.\d{6}$`)

// Proxy an OpenAI-chat and an Anthropic-messages call (streamed and not) through /w/{id} and read
// them back through the control plane: sessions, session detail, stats, Setup.status and /statusz.
func TestProxiedCallsAppearInSessionsStatsSetupAndStatusz(t *testing.T) {
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	p := bootPlane(t, dataplane.Deps{Resolver: bothWires{base}}, nil)

	var before contract.Setup
	p.getJSON(t, "/api/workspaces/"+p.ws.ID+"/setup", &before)
	if before.Status.FirstCallAt != nil || before.Status.CallsToday != 0 {
		t.Fatalf("setup before any call: %+v", before.Status)
	}

	runs := []struct{ fixture, session string }{
		{"openai/chat-stream-text", "e2e-openai"}, {"openai/chat", "e2e-openai"},
		{"anthropic/messages-stream-text", "e2e-anthropic"}, {"anthropic/messages", "e2e-anthropic"},
	}
	for _, r := range runs {
		want, _ := testkit.Fixture(r.fixture)
		if code, _, got := p.call(t, p.ws.PublicID, r.fixture, r.session); code != 200 || !bytes.Equal(got, want) {
			t.Fatalf("%s: %d, %d bytes (want %d)", r.fixture, code, len(got), len(want))
		}
	}
	for _, req := range up.Requests() {
		if req.Header.Get("x-meterwise-session") != "" {
			t.Fatalf("the session header reached the upstream: %v", req.Header)
		}
	}
	p.waitCalls(t, len(runs))

	var list struct {
		Items      []contract.SessionView `json:"items"`
		NextCursor *string                `json:"nextCursor"`
	}
	p.getJSON(t, "/api/workspaces/"+p.ws.ID+"/sessions", &list)
	if len(list.Items) != 2 {
		t.Fatalf("sessions: %+v", list.Items)
	}
	for _, s := range list.Items {
		if s.Calls != 2 || s.WorkspaceID != p.ws.ID || s.Source != "header" {
			t.Fatalf("session %+v", s)
		}
		var detail struct {
			Session contract.SessionView `json:"session"`
			Calls   []contract.CallView  `json:"calls"`
		}
		p.getJSON(t, "/api/sessions/"+s.ID+"?workspaceId="+p.ws.ID, &detail)
		if len(detail.Calls) != 2 || detail.Calls[0].CallIndex != 1 || detail.Calls[1].CallIndex != 2 {
			t.Fatalf("detail %+v", detail)
		}
		streamed := 0
		for _, c := range detail.Calls {
			if c.Stream {
				streamed++
			}
			if c.Status != 200 || c.InputTokens <= 0 || c.OutputTokens <= 0 {
				t.Fatalf("call %+v", c)
			}
		}
		if streamed != 1 {
			t.Fatalf("want one streamed and one plain call per session, got %d streamed", streamed)
		}
	}
	// a session from another workspace is a 404, never a read
	if code, _ := get(t, p.api+"/api/sessions/"+list.Items[0].ID+"?workspaceId=00000000-0000-0000-0000-000000000000"); code != 404 && code != 403 {
		t.Fatalf("cross-workspace read: %d", code)
	}

	// money is a decimal string on the wire, never a number
	_, raw := get(t, p.api+"/api/workspaces/"+p.ws.ID+"/stats?range=7d")
	var stats map[string]any
	if err := json.Unmarshal([]byte(raw), &stats); err != nil {
		t.Fatal(err)
	}
	spend, isString := stats["spendUsd"].(string)
	if !isString || !microUSDString.MatchString(spend) || stats["calls"] != float64(4) || stats["sessions"] != float64(2) || stats["avgCallsPerSession"] != float64(2) {
		t.Fatalf("stats: %s", raw)
	}
	if byDay, _ := stats["byDay"].([]any); len(byDay) == 0 {
		t.Fatalf("stats byDay empty: %s", raw)
	}
	_, rawSessions := get(t, p.api+"/api/workspaces/"+p.ws.ID+"/sessions")
	if !regexp.MustCompile(`"costUsd":"\d+\.\d{6}"`).MatchString(rawSessions) {
		t.Fatalf("session costUsd is not a decimal string: %s", rawSessions)
	}

	// Setup.status flips to connected
	var after contract.Setup
	p.getJSON(t, "/api/workspaces/"+p.ws.ID+"/setup", &after)
	if after.Status.FirstCallAt == nil || after.Status.LastCallAt == nil || after.Status.CallsToday != 4 {
		t.Fatalf("setup after calls: %+v", after.Status)
	}

	// /statusz shows the ledger, the directory and admission
	code, body := get(t, p.admin+"/statusz")
	var doc struct {
		Sources map[string]map[string]any `json:"sources"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &doc) != nil {
		t.Fatalf("statusz %d %s", code, body)
	}
	for _, name := range []string{"ledger", "directory", "admission"} {
		if doc.Sources[name] == nil {
			t.Fatalf("statusz has no %q source: %s", name, body)
		}
	}
	if doc.Sources["ledger"]["flushed"] != float64(4) || doc.Sources["ledger"]["dropped"] != float64(0) {
		t.Fatalf("ledger status %v", doc.Sources["ledger"])
	}
}

func TestProxyRefusalsAre404ThenGraceThen410AndNoLogCarriesAPublicID(t *testing.T) {
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	p := bootPlane(t, dataplane.Deps{Resolver: bothWires{base}}, nil)
	const fixture = "openai/chat"
	if code, _, _ := p.call(t, strings.Repeat("A", 22), fixture, ""); code != 404 {
		t.Fatalf("unknown id: %d", code)
	}
	admin := contract.Principal{OrgID: store.OrgID(), Role: contract.RoleAdmin}
	old := p.ws.PublicID
	res, err := p.rt.svc.Directory().Reset(context.Background(), admin, p.ws.ID, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, _ := p.call(t, old, fixture, ""); code != 200 {
		t.Fatalf("retired id inside grace: %d", code)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, p.api+"/api/workspaces/"+p.ws.ID+"/url/retire-previous", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 204 {
		t.Fatalf("retire-previous: %v %v", resp, err)
	}
	resp.Body.Close()
	code, hdr, body := p.call(t, old, fixture, "")
	if code != 410 || !strings.Contains(hdr.Get("Content-Type"), "json") || !bytes.Contains(body, []byte("error")) {
		t.Fatalf("after grace: %d %s %s", code, hdr.Get("Content-Type"), body)
	}
	_, _ = get(t, p.api+"/w/"+old+"/x")
	p.stop()
	out := p.logs.String()
	for _, id := range []string{old, res.Workspace.PublicID} {
		if strings.Contains(out, id) {
			t.Fatalf("a log line carries the public id %q", id)
		}
	}
}

// A workspace at its concurrency cap answers 429 with Retry-After once the queue timeout passes.
func TestAWorkspaceAtItsCapAnswers429WithRetryAfter(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(release) })
	base, _ := url.Parse(slow.URL)
	adm, err := dataplane.NewAdmission(1, 50*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	p := bootPlane(t, dataplane.Deps{Resolver: bothWires{base}, Admission: adm}, nil)
	first := make(chan int, 1)
	go func() { c, _, _ := p.call(t, p.ws.PublicID, "openai/chat", ""); first <- c }()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if adm.Status()["in_flight"] == int64(1) {
			break
		}
	}
	code, hdr, _ := p.call(t, p.ws.PublicID, "openai/chat", "")
	if code != 429 || hdr.Get("Retry-After") == "" {
		t.Fatalf("over the cap: %d Retry-After=%q", code, hdr.Get("Retry-After"))
	}
}
