package dataplane_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/dataplane"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

const (
	curID   = "Aa0Bb1Cc2Dd3Ee4Ff5Gg6H"
	oldID   = "Zz9Yy8Xx7Ww6Vv5Uu4Tt3S"
	resetID = "Qq1Ww2Ee3Rr4Tt5Yy6Uu7I"
	openID  = "Oo1Pp2Ee3Nn4Aa5Ii6Zz7A" // allowedProviders: ["openai"]
	capID   = "Cc1Aa2Pp3Tt4Ee5St6Ww7B" // maxConcurrent override (settable)
	wsID    = "11111111-1111-1111-1111-111111111111"
)

// directory is a fake workspace directory whose settings tests can change between calls.
type directory struct {
	mu     sync.Mutex
	capSet map[string]any
}

func (d *directory) lookup(_ context.Context, id string) (contract.Workspace, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws := contract.Workspace{ID: wsID, OrgID: "org", PublicID: id, Settings: map[string]any{}}
	switch id {
	case curID:
	case oldID:
		ws.Retired = true
	case resetID:
		return contract.Workspace{}, contract.NewError(contract.CodeWorkspaceURLReset, "reset")
	case openID:
		ws.Settings = map[string]any{"allowedProviders": []any{"openai"}}
	case capID:
		ws.ID = "22222222-2222-2222-2222-222222222222"
		ws.Settings = d.capSet
	default:
		return contract.Workspace{}, contract.NewError(contract.CodeNotFound, "unknown")
	}
	return ws, nil
}

type callSpy struct {
	tollgate.NoopHook
	mu    sync.Mutex
	calls []tollgate.Call
}

func (*callSpy) Name() string { return "spy" }
func (s *callSpy) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta := map[string]any{}
	for k, v := range c.Meta {
		meta[k] = v
	}
	s.calls = append(s.calls, tollgate.Call{Tenant: c.Tenant, Meta: meta, SessionHeader: c.SessionHeader})
	return nil, nil
}

type rig struct {
	srv  *httptest.Server
	up   *testkit.FakeUpstream
	dir  *directory
	spy  *callSpy
	base *url.URL
}

type rigOpts struct {
	up       testkit.Options
	max      int
	queue    time.Duration
	devBare  bool
	notFront bool
}

func newRig(t *testing.T, o rigOpts) *rig {
	t.Helper()
	up := testkit.NewFakeUpstream(t, o.up)
	base, _ := url.Parse(up.URL)
	provs := map[string]tollgate.Provider{
		"openai":    {Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: base},
		"anthropic": {Name: "anthropic", Wire: tollgate.WireAnthropicMessages, BaseURL: base},
	}
	if o.max == 0 {
		o.max = 8
	}
	if o.queue == 0 {
		o.queue = time.Second
	}
	adm, err := dataplane.NewAdmission(o.max, o.queue)
	if err != nil {
		t.Fatal(err)
	}
	d := &directory{}
	auth := wsauth.New(wsauth.Uncached(d.lookup), 0)
	if o.devBare {
		auth = auth.WithDevTenant(tollgate.Tenant{ID: "dev-ws", Attrs: map[string]string{}})
	}
	spy := &callSpy{}
	p, err := dataplane.New(cfg(t, map[string]string{}), dataplane.Deps{
		Auth: auth, Resolver: dataplane.NewWorkspaceResolver(provs, o.devBare), Admission: adm,
		Hooks: []tollgate.Hook{wsauth.Hook{}, spy},
	})
	if err != nil {
		t.Fatal(err)
	}
	h := http.Handler(p.Handler())
	if !o.notFront {
		h = wsauth.Front(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &rig{srv: srv, up: up, dir: d, spy: spy, base: base}
}

func (r *rig) do(t *testing.T, path, fixture string, hdr map[string]string) (*http.Response, string) {
	t.Helper()
	_, body := testkit.RequestFor(fixture)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, r.srv.URL+path, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testkit.FixtureHeader, fixture)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

var wires = []struct {
	name, fixture, path string
	hdr                 map[string]string
	credHeader          string
	shapeMarker         string // in a provider-shaped error body
}{
	{"openai", "openai/chat-stream-text", "/openai/v1/chat/completions", map[string]string{"Authorization": "Bearer sk-live-secret"}, "Authorization", `"param"`},
	{"anthropic", "anthropic/messages-stream-tool-use", "/anthropic/v1/messages", map[string]string{"x-api-key": "sk-ant-live-secret", "anthropic-version": "2023-06-01"}, "x-api-key", `"type":"error"`},
}

func TestBothWiresStreamThroughTheWorkspaceURLWithTheClientsOwnKey(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			r := newRig(t, rigOpts{})
			resp, body := r.do(t, "/w/"+curID+w.path, w.fixture, w.hdr)
			if resp.StatusCode != 200 || !strings.Contains(body, "data:") {
				t.Fatalf("status %d body %.80q", resp.StatusCode, body)
			}
			want, _ := testkit.Fixture(w.fixture)
			if body != string(want) {
				t.Fatalf("the stream was altered: %d bytes, want %d", len(body), len(want))
			}
			reqs := r.up.Requests()
			if len(reqs) != 1 || reqs[0].Path != testkit.PathFor(w.fixture) {
				t.Fatalf("upstream saw %+v, want one call to %s (no /w/{id}, no provider prefix)", reqs, testkit.PathFor(w.fixture))
			}
			if got := reqs[0].Header.Get(w.credHeader); got != w.hdr[w.credHeader] {
				t.Fatalf("%s reached the upstream as %q", w.credHeader, got)
			}
		})
	}
}

func TestRetiredInGraceStreamsAndIsFlagged(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			r := newRig(t, rigOpts{})
			if resp, _ := r.do(t, "/w/"+oldID+w.path, w.fixture, w.hdr); resp.StatusCode != 200 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			if len(r.spy.calls) != 1 || r.spy.calls[0].Meta[wsauth.MetaURLRetired] != "true" {
				t.Fatalf("flag missing: %+v", r.spy.calls)
			}
		})
	}
}

func TestRefusalsAreProviderShapedAndNeverReachTheUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		want     int
	}{{"unknown", strings.Repeat("A", 22), 404}, {"reset past grace", resetID, 410}} {
		for _, w := range wires {
			t.Run(tc.name+"/"+w.name, func(t *testing.T) {
				for _, front := range []bool{true, false} {
					r := newRig(t, rigOpts{notFront: !front})
					resp, body := r.do(t, "/w/"+tc.id+w.path, w.fixture, w.hdr)
					if resp.StatusCode != tc.want {
						t.Fatalf("front=%v status %d %q, want %d", front, resp.StatusCode, body, tc.want)
					}
					if front && !strings.Contains(strings.ReplaceAll(body, " ", ""), strings.ReplaceAll(w.shapeMarker, " ", "")) {
						t.Fatalf("not %s-shaped: %s", w.name, body)
					}
					for _, leak := range []string{tc.id, "sk-live-secret", "sk-ant-live-secret"} {
						if strings.Contains(body, leak) {
							t.Fatalf("the body leaks %q: %s", leak, body)
						}
					}
					if len(r.up.Requests()) != 0 || len(r.spy.calls) != 0 {
						t.Fatal("a refused call reached the upstream or a hook")
					}
				}
			})
		}
	}
}

func headerNames(h http.Header) []string {
	var out []string
	for k := range h {
		out = append(out, strings.ToLower(k))
	}
	sort.Strings(out)
	return out
}

func TestSessionHeaderNeverReachesTheUpstreamAndNothingElseChanges(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			r := newRig(t, rigOpts{})
			sent := map[string]string{"x-custom-trace": "abc", "User-Agent": "opencode/1.2"}
			for k, v := range w.hdr {
				sent[k] = v
			}
			// the same request straight to the fake is the baseline of what the client itself sends
			_, body := testkit.RequestFor(w.fixture)
			direct, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, r.up.URL+testkit.PathFor(w.fixture), strings.NewReader(string(body)))
			direct.Header.Set("Content-Type", "application/json")
			direct.Header.Set(testkit.FixtureHeader, w.fixture)
			for k, v := range sent {
				direct.Header.Set(k, v)
			}
			if resp, err := http.DefaultClient.Do(direct); err != nil {
				t.Fatal(err)
			} else {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
			tagged := map[string]string{dataplane.SessionHeader: "run-42"}
			for k, v := range sent {
				tagged[k] = v
			}
			if resp, _ := r.do(t, "/w/"+curID+w.path, w.fixture, tagged); resp.StatusCode != 200 {
				t.Fatalf("status %d", resp.StatusCode)
			}
			reqs := r.up.Requests()
			if len(reqs) != 2 {
				t.Fatalf("upstream saw %d requests", len(reqs))
			}
			base, via := reqs[0].Header, reqs[1].Header
			if via.Get(dataplane.SessionHeader) != "" {
				t.Fatal("x-meterwise-session reached the upstream")
			}
			if got, want := strings.Join(headerNames(via), ","), strings.Join(headerNames(base), ","); got != want {
				t.Fatalf("header names via the proxy\n  %s\nbut direct\n  %s", got, want)
			}
			for k := range sent {
				if via.Get(k) != base.Get(k) {
					t.Errorf("%s changed: %q -> %q", k, base.Get(k), via.Get(k))
				}
			}
			if len(r.spy.calls) != 1 || r.spy.calls[0].SessionHeader != "run-42" {
				t.Fatalf("the hook must see the session header: %+v", r.spy.calls)
			}
		})
	}
}

func TestAProviderOutsideTheWorkspacesAllowListIs403ForThatProvider(t *testing.T) {
	r := newRig(t, rigOpts{})
	if resp, _ := r.do(t, "/w/"+openID+wires[0].path, wires[0].fixture, wires[0].hdr); resp.StatusCode != 200 {
		t.Fatalf("an allowed provider: %d", resp.StatusCode)
	}
	resp, body := r.do(t, "/w/"+openID+wires[1].path, wires[1].fixture, wires[1].hdr)
	if resp.StatusCode != 403 || !strings.Contains(body, `"type":"error"`) || !strings.Contains(body, "permission_error") {
		t.Fatalf("status %d body %s, want a 403 in Anthropic's shape", resp.StatusCode, body)
	}
	if n := len(r.up.Requests()); n != 1 {
		t.Fatalf("upstream saw %d, want only the allowed call", n)
	}
}

func TestABareProviderPathIs404UnlessDev(t *testing.T) {
	for _, dev := range []bool{false, true} {
		r := newRig(t, rigOpts{devBare: dev})
		resp, _ := r.do(t, wires[0].path, wires[0].fixture, wires[0].hdr)
		if dev && resp.StatusCode != 200 {
			t.Errorf("dev: bare path answered %d, want 200", resp.StatusCode)
		}
		if !dev && (resp.StatusCode != 404 || len(r.up.Requests()) != 0) {
			t.Errorf("prod: bare path answered %d with %d upstream calls, want 404 and none", resp.StatusCode, len(r.up.Requests()))
		}
	}
}

func TestTheResolverAloneRefusesABareProviderPathOutsideDev(t *testing.T) {
	res := dataplane.NewWorkspaceResolver(nil, false)
	req := httptest.NewRequest("POST", "/openai/v1/chat/completions", nil)
	if _, _, err := res.Resolve(context.Background(), tollgate.Tenant{ID: "x"}, req); err == nil {
		t.Fatal("a tenant with no workspace url resolved a provider")
	}
	ten := tollgate.Tenant{ID: "x", Attrs: map[string]string{wsauth.AttrPublicID: curID}}
	for _, path := range []string{"/openai/v1/chat/completions", "/w/" + curID + "/openai/v1/chat/completions"} {
		p, rest, err := res.Resolve(context.Background(), ten, httptest.NewRequest("POST", path, nil))
		if err != nil || p.Name != "openai" || rest != "/v1/chat/completions" || p.BaseURL.Host != "api.openai.com" {
			t.Errorf("%s: %+v %q %v", path, p, rest, err)
		}
	}
	if _, _, err := res.Resolve(context.Background(), ten, httptest.NewRequest("POST", "/nope/v1", nil)); err == nil {
		t.Fatal("unknown provider resolved")
	}
}

func tollgateTenant(id, maxConcurrent string) tollgate.Tenant {
	return tollgate.Tenant{ID: id, Attrs: map[string]string{wsauth.AttrMaxConcurrent: maxConcurrent}}
}
