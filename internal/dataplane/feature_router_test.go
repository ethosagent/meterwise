package dataplane_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/router"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

func routerTables() contract.RouterTables {
	t, _ := router.BuildTables(
		[]contract.RoutePair{
			{Provider: "openai", From: "gpt-4.1", To: "gpt-4.1-mini"},
			{Provider: "anthropic", From: "claude-opus-4-5", To: "claude-haiku-4-5"},
		},
		[]router.CapRow{
			{Provider: "openai", Model: "gpt-4.1-mini", JSONSchema: true, Vision: true},
			{Provider: "anthropic", Model: "claude-haiku-4-5", MaxTools: new(0), Vision: true},
		},
		everyModelPriced{}, time.Unix(0, 0))
	return t
}

type everyModelPriced struct{}

func (everyModelPriced) Cost(string, string, contract.Usage, time.Time) (contract.MicroUSD, bool) {
	return 1, true
}

type tenantAuth struct{ attrs map[string]string }

func (a tenantAuth) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	return tollgate.Tenant{ID: "ws", Attrs: a.attrs}, nil
}

// decisionTap records the router's decision for each call, as an injected hook after the router sees it.
type decisionTap struct {
	tollgate.NoopHook
	mu   sync.Mutex
	seen []contract.RouteDecision
	fire map[int]bool // session call indexes on which to add a monitor verdict
}

func (*decisionTap) Name() string { return "tap" }

func (d *decisionTap) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rd, _ := c.Meta[contract.MetaRouter].(contract.RouteDecision)
	d.seen = append(d.seen, rd)
	if v, ok := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts); ok && d.fire[c.Session.Calls] {
		v.Add(contract.MonitorVerdict{Monitor: contract.MonitorDuplicateToolCall, Mode: contract.ModeObserve, Action: contract.ModeObserve})
	}
	return nil, nil
}

type routerStack struct {
	srv  *httptest.Server
	srcs []contract.StatusSource
}

// newRouterStack serves the real hook chain (wsauth, history, router, monitors, injected, a no-op ledger) in front of
// upstream, for a tenant whose router setting is rc.
func newRouterStack(t *testing.T, upstream string, rc contract.RouterConfig, injected ...tollgate.Hook) *routerStack {
	t.Helper()
	base, _ := url.Parse(upstream)
	provs := map[string]tollgate.Provider{
		"openai":    {Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: base},
		"anthropic": {Name: "anthropic", Wire: tollgate.WireAnthropicMessages, BaseURL: base},
	}
	hooks, srcs := dataplane.HookChain(dataplane.ChainOptions{PrivacyMode: true, Router: routerTables()}, injected, namedHook{name: "ledger"})
	p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{
		Auth: tenantAuth{map[string]string{wsauth.AttrRouter: rc.Attr()}}, Resolver: dataplane.NewWorkspaceResolver(provs, true), Hooks: hooks,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(func() { srv.Close(); _ = p.Close(context.Background()) })
	return &routerStack{srv: srv, srcs: srcs}
}

func (s *routerStack) post(t *testing.T, path, session, body, fixture string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, s.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-session-id", session)
	if fixture != "" {
		req.Header.Set(testkit.FixtureHeader, fixture)
	}
	resp, err := s.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

// openAIAt is the body of call n of a session whose agent has read a file in every earlier turn.
func openAIAt(model string, n int, extra string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `{"model":%q,"stream":false%s,"messages":[{"role":"system","content":"You are a demo agent."},{"role":"user","content":"fix it"}`, model, extra)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, `,{"role":"assistant","content":null,"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]},{"role":"tool","tool_call_id":"c%d","content":"package f%d"}`, i, i, i, i)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

func anthropicAt(model string, n int, extra string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `{"model":%q,"max_tokens":64%s,"messages":[{"role":"user","content":"fix it"}`, model, extra)
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, `,{"role":"assistant","content":[{"type":"tool_use","id":"t%d","name":"read_file","input":{"path":"f%d.go"}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"t%d","content":"package f%d"}]}`, i, i, i, i)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

func models(up *testkit.FakeUpstream) []string {
	var out []string
	for _, r := range up.Requests() {
		i := bytes.Index(r.Body, []byte(`"model":"`))
		rest := string(r.Body[i+9:])
		out = append(out, rest[:strings.IndexByte(rest, '"')])
	}
	return out
}

const (
	oaPath = "/openai/v1/chat/completions"
	anPath = "/anthropic/v1/messages"
)

// Conservative mode rewrites only the model, on both wires, through the real chain: the upstream's body equals the
// client's with the one string swapped, from the third call on.
func TestConservativeChangesOnlyTheModelOnBothWires(t *testing.T) {
	for _, w := range []struct {
		name, path, session, fixture, from, to string
		body                                   func(model string, n int, extra string) string
	}{
		{"openai", oaPath, "oa", "openai/chat", "gpt-4.1", "gpt-4.1-mini", openAIAt},
		{"openai dated id", oaPath, "oa-dated", "openai/chat", "gpt-4.1-2025-04-14", "gpt-4.1-mini", openAIAt},
		{"anthropic", anPath, "an", "anthropic/messages", "claude-opus-4-5", "claude-haiku-4-5", anthropicAt},
	} {
		t.Run(w.name, func(t *testing.T) {
			up := testkit.NewFakeUpstream(t, testkit.Options{})
			tap := &decisionTap{}
			s := newRouterStack(t, up.URL, contract.RouterConfig{Mode: contract.RouterModeConservative}, tap)
			var sent []string
			for n := 1; n <= 4; n++ {
				body := w.body(w.from, n, "")
				sent = append(sent, body)
				if code := s.post(t, w.path, w.session, body, w.fixture); code != 200 {
					t.Fatalf("call %d: status %d", n, code)
				}
			}
			got := up.Requests()
			if len(got) != 4 {
				t.Fatalf("upstream saw %d requests", len(got))
			}
			for i, r := range got {
				want := sent[i]
				if i >= 2 { // the first two calls are held; the rest route
					want = strings.Replace(sent[i], `"model":"`+w.from+`"`, `"model":"`+w.to+`"`, 1)
				}
				if string(r.Body) != want {
					t.Errorf("call %d upstream body:\n%s\nwant\n%s", i+1, r.Body, want)
				}
			}
			for i, d := range tap.seen[:2] {
				if d.Action != contract.RouteHeld || d.Reason != contract.HeldFirstSteps {
					t.Errorf("call %d: %+v", i+1, d)
				}
			}
			if d := tap.seen[2]; d.Action != contract.RouteRouted || d.From != w.from || d.To != w.to || d.ScoreBp != 2500 {
				t.Errorf("call 3: %+v", d)
			}
		})
	}
}

// Observe records the would-route decision and sends the client's bytes untouched.
func TestObserveSendsTheClientsBytesAndRecordsWouldRoute(t *testing.T) {
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	tap := &decisionTap{}
	s := newRouterStack(t, up.URL, contract.RouterConfig{Mode: contract.RouterModeObserve}, tap)
	var sent []string
	for n := 1; n <= 4; n++ {
		body := openAIAt("gpt-4.1", n, "")
		sent = append(sent, body)
		s.post(t, oaPath, "obs", body, "openai/chat")
	}
	for i, r := range up.Requests() {
		if string(r.Body) != sent[i] {
			t.Errorf("call %d: observe changed the body", i+1)
		}
	}
	if d := tap.seen[3]; d.Action != contract.RouteObserve || d.To != "gpt-4.1-mini" || d.Mode != contract.RouterModeObserve {
		t.Errorf("call 4 decision = %+v", d)
	}
}

// Requests the target cannot serve stay on the model they asked for, on both wires.
func TestCapabilityGuardHoldsOnBothWires(t *testing.T) {
	cases := []struct {
		name, path, model, fixture string
		body                       func(string, int, string) string
		extra                      string
		routes                     bool
	}{
		{"openai plain routes", oaPath, "gpt-4.1", "openai/chat", openAIAt, "", true},
		{"openai json schema routes (target supports it)", oaPath, "gpt-4.1", "openai/chat", openAIAt, `,"response_format":{"type":"json_schema","json_schema":{"name":"x","schema":{}}}`, true},
		{"openai reasoning is held", oaPath, "gpt-4.1", "openai/chat", openAIAt, `,"reasoning_effort":"high"`, false},
		{"anthropic plain routes", anPath, "claude-opus-4-5", "anthropic/messages", anthropicAt, "", true},
		{"anthropic tools are held (max_tools 0)", anPath, "claude-opus-4-5", "anthropic/messages", anthropicAt, `,"tools":[{"name":"read_file","input_schema":{"type":"object"}}]`, false},
		{"anthropic json output is held", anPath, "claude-opus-4-5", "anthropic/messages", anthropicAt, `,"output_format":{"type":"json_schema","schema":{}}`, false},
		{"anthropic thinking is held", anPath, "claude-opus-4-5", "anthropic/messages", anthropicAt, `,"thinking":{"type":"enabled","budget_tokens":1024}`, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := testkit.NewFakeUpstream(t, testkit.Options{})
			s := newRouterStack(t, up.URL, contract.RouterConfig{Mode: contract.RouterModeConservative})
			var last string
			for n := 1; n <= 3; n++ {
				last = tc.body(tc.model, n, tc.extra)
				s.post(t, tc.path, fmt.Sprintf("g%d", i), last, tc.fixture)
			}
			got := up.Requests()[2].Body
			routed := !bytes.Equal(got, []byte(last))
			if routed != tc.routes {
				t.Errorf("routed = %v, want %v; upstream saw %s", routed, tc.routes, got)
			}
			if !routed && !bytes.Equal(got, []byte(last)) {
				t.Error("a held call must reach the upstream byte for byte")
			}
		})
	}
}

// A verdict on a routed call keeps the next two calls on the strong model, through the real chain.
func TestEscalationThroughTheChain(t *testing.T) {
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	tap := &decisionTap{fire: map[int]bool{3: true}} // the monitors "fire" on call 3, which was routed
	s := newRouterStack(t, up.URL, contract.RouterConfig{Mode: contract.RouterModeConservative}, tap)
	for n := 1; n <= 7; n++ {
		if code := s.post(t, oaPath, "esc", openAIAt("gpt-4.1", n, ""), "openai/chat"); code != 200 {
			t.Fatalf("call %d: %d", n, code)
		}
	}
	want := []string{"gpt-4.1", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1", "gpt-4.1", "gpt-4.1-mini", "gpt-4.1-mini"}
	if got := models(up); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("models at the upstream = %v\nwant %v", got, want)
	}
	if d := tap.seen[3]; d.Reason != contract.HeldEscalated {
		t.Errorf("call 4 = %+v", d)
	}
}

// An upstream that refuses the routed model (404) marks the pair unavailable: the client sees that one error and
// the next call goes to the model it asked for (ruling A1).
func TestRefusedTargetMarksThePairUnavailableThroughTheChain(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		model := "gpt-4.1"
		if bytes.Contains(b, []byte(`"model":"gpt-4.1-mini"`)) {
			model = "gpt-4.1-mini"
		}
		mu.Lock()
		seen = append(seen, model)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if model == "gpt-4.1-mini" {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":{"message":"The model does not exist or you do not have access to it.","type":"invalid_request_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"gpt-4.1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`))
	}))
	t.Cleanup(up.Close)
	s := newRouterStack(t, up.URL, contract.RouterConfig{Mode: contract.RouterModeConservative})
	codes := make([]int, 0, 5)
	for n := 1; n <= 5; n++ {
		codes = append(codes, s.post(t, oaPath, "unav", openAIAt("gpt-4.1", n, ""), ""))
		if n == 3 {
			waitFor(t, func() bool { return routerStatus(s)["unavailable_marked"] == int64(1) })
		}
	}
	if fmt.Sprint(codes) != "[200 200 404 200 200]" {
		t.Errorf("client statuses = %v: the client sees exactly the one refusal", codes)
	}
	if got := fmt.Sprint(seen); got != "[gpt-4.1 gpt-4.1 gpt-4.1-mini gpt-4.1 gpt-4.1]" {
		t.Errorf("upstream models = %s", got)
	}
}

func routerStatus(s *routerStack) map[string]any {
	for _, src := range s.srcs {
		if st := src.Status(); st["unavailable_marked"] != nil {
			return st
		}
	}
	return map[string]any{}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met in 5s")
}

func TestRouterSlotSitsBetweenMemoryAndMonitorsAndOnlyWithTables(t *testing.T) {
	hooks, srcs := dataplane.HookChain(dataplane.ChainOptions{Router: routerTables()}, nil, namedHook{name: "ledger"})
	// trim (Plan 11) is always in the chain, before the router (R4 order).
	if got, want := fmt.Sprint(names(hooks)), "[wsauth history trim router monitors ledger]"; got != want {
		t.Errorf("chain = %s, want %s", got, want)
	}
	if len(srcs) != 4 {
		t.Errorf("history, trim, router and monitors report status, got %d sources", len(srcs))
	}
	hooks, _ = dataplane.HookChain(dataplane.ChainOptions{Router: contract.RouterTables{Routes: []contract.RoutePair{{Provider: "p", From: "a", To: "b"}}}}, nil, namedHook{name: "ledger"})
	if got := fmt.Sprint(names(hooks)); got != "[wsauth history trim monitors ledger]" {
		t.Errorf("tables with no capability rows route nothing, so the slot stays empty: %s", got)
	}
	if router.AttrRouter != wsauth.AttrRouter {
		t.Errorf("router.AttrRouter %q must equal wsauth.AttrRouter %q", router.AttrRouter, wsauth.AttrRouter)
	}
}
