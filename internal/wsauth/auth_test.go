package wsauth_test

import (
	"context"
	"errors"
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
	"github.com/ethosagent/meterwise/internal/wsauth"
)

const (
	curID   = "Aa0Bb1Cc2Dd3Ee4Ff5Gg6H"
	oldID   = "Zz9Yy8Xx7Ww6Vv5Uu4Tt3S"
	resetID = "Qq1Ww2Ee3Rr4Tt5Yy6Uu7I"
	cidrID  = "Mm1Nn2Bb3Vv4Cc5Xx6Zz7L"
	orgID   = "00000000-0000-0000-0000-000000000001"
	wsID    = "11111111-1111-1111-1111-111111111111"
)

var _ tollgate.InlineSafe = (*wsauth.Authenticator)(nil)

// dir is a directory of fixed answers that counts lookups.
type dir struct {
	mu    sync.Mutex
	calls int
	block bool
	fail  error
}

func (d *dir) ByPublicID(ctx context.Context, id string) (contract.Workspace, error) {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	if d.block {
		<-ctx.Done()
		return contract.Workspace{}, ctx.Err()
	}
	if d.fail != nil {
		return contract.Workspace{}, d.fail
	}
	ws := contract.Workspace{ID: wsID, OrgID: orgID, PublicID: id, Name: "w", Settings: map[string]any{}}
	switch id {
	case curID:
	case oldID:
		ws.Retired = true
	case resetID:
		return contract.Workspace{}, contract.NewError(contract.CodeWorkspaceURLReset, "reset")
	case cidrID:
		ws.AllowedClientCIDRs = []string{"203.0.113.0/24", "2001:db8::/32"}
	default:
		return contract.Workspace{}, contract.NewError(contract.CodeNotFound, "no such workspace")
	}
	return ws, nil
}

func authReq(path, remote string) *http.Request {
	r := httptest.NewRequest("POST", path, nil)
	r.RemoteAddr = remote
	r.Header.Set("Authorization", "Bearer sk-live-secret")
	return r
}

func statusOf(err error) int {
	var se tollgate.StatusError
	if errors.As(err, &se) {
		return se.HTTPStatus()
	}
	return 0
}

func TestAuthenticateOutcomes(t *testing.T) {
	d := &dir{}
	a := wsauth.New(wsauth.Uncached(d.ByPublicID), 0)
	for _, tc := range []struct {
		name, path, remote string
		status             int // 0 = authenticated
		retired            bool
	}{
		{"current id", "/w/" + curID + "/v1/chat/completions", "198.51.100.1:5000", 0, false},
		{"current id, no rest", "/w/" + curID, "198.51.100.1:5000", 0, false},
		{"retired id in grace", "/w/" + oldID + "/v1/messages", "198.51.100.1:5000", 0, true},
		{"unknown id", "/w/" + strings.Repeat("A", 22) + "/v1/messages", "198.51.100.1:5000", 404, false},
		{"reset id past grace", "/w/" + resetID + "/v1/messages", "198.51.100.1:5000", 410, false},
		{"not a workspace path", "/openai/v1/chat/completions", "198.51.100.1:5000", 404, false},
		{"short id", "/w/short/v1/messages", "198.51.100.1:5000", 404, false},
		{"id with a bad character", "/w/" + curID[:21] + "-/v1/messages", "198.51.100.1:5000", 404, false},
		{"cidr: inside v4", "/w/" + cidrID + "/v1/messages", "203.0.113.77:1", 0, false},
		{"cidr: inside v6", "/w/" + cidrID + "/v1/messages", "[2001:db8::5]:1", 0, false},
		{"cidr: outside", "/w/" + cidrID + "/v1/messages", "198.51.100.1:5000", 403, false},
		{"cidr: unparsable peer", "/w/" + cidrID + "/v1/messages", "garbage", 403, false},
	} {
		ten, err := a.Authenticate(context.Background(), authReq(tc.path, tc.remote))
		if got := statusOf(err); got != tc.status {
			t.Errorf("%s: status %d (%v), want %d", tc.name, got, err, tc.status)
			continue
		}
		if tc.status != 0 {
			continue
		}
		id := tc.path[len("/w/") : len("/w/")+22]
		if ten.ID != wsID || ten.Attrs["org_id"] != orgID || ten.Attrs["public_id"] != id {
			t.Errorf("%s: tenant %+v", tc.name, ten)
		}
		if (ten.Attrs["url_retired"] == "true") != tc.retired {
			t.Errorf("%s: url_retired = %q, want %v", tc.name, ten.Attrs["url_retired"], tc.retired)
		}
	}
	if d.calls == 0 {
		t.Fatal("the directory was never asked")
	}
}

func TestMalformedIDsNeverReachTheDirectory(t *testing.T) {
	d := &dir{}
	a := wsauth.New(wsauth.Uncached(d.ByPublicID), 0)
	for _, p := range []string{"/", "/w/", "/w//x", "/w/x", "/x/w/" + curID, "/w/" + curID + "x/v1"} {
		if _, err := a.Authenticate(context.Background(), authReq(p, "198.51.100.1:1")); statusOf(err) != 404 {
			t.Errorf("%s: %v", p, err)
		}
	}
	if d.calls != 0 {
		t.Fatalf("directory asked %d times for a malformed id", d.calls)
	}
}

func TestLookupFailureAndTimeoutAre503(t *testing.T) {
	a := wsauth.New(wsauth.Uncached((&dir{fail: errors.New("pg down")}).ByPublicID), 0)
	if _, err := a.Authenticate(context.Background(), authReq("/w/"+curID+"/x", "198.51.100.1:1")); statusOf(err) != 503 {
		t.Fatalf("failure: %v", err)
	}
	slow := wsauth.New(wsauth.Uncached((&dir{block: true}).ByPublicID), 30*time.Millisecond)
	start := time.Now()
	_, err := slow.Authenticate(context.Background(), authReq("/w/"+curID+"/x", "198.51.100.1:1"))
	if statusOf(err) != 503 || time.Since(start) > time.Second {
		t.Fatalf("timeout: %v after %v", err, time.Since(start))
	}
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		path, id, rest string
		ok             bool
	}{
		{"/w/" + curID + "/v1/chat/completions", curID, "/v1/chat/completions", true},
		{"/w/" + curID, curID, "/", true},
		{"/openai/v1", "", "", false},
		{"/w/short/v1", "", "", false},
	} {
		id, rest, ok := wsauth.Parse(tc.path)
		if id != tc.id || rest != tc.rest || ok != tc.ok {
			t.Errorf("Parse(%q) = %q %q %v", tc.path, id, rest, ok)
		}
	}
}

func TestPolicyOf(t *testing.T) {
	if p := wsauth.PolicyOf(nil); p != nil {
		t.Fatalf("empty settings: %+v", p)
	}
	if p := wsauth.PolicyOf(map[string]any{"mode": "observe", "capture": "yes"}); p != nil {
		t.Fatalf("unknown or mistyped keys set a policy: %+v", p)
	}
	p := wsauth.PolicyOf(map[string]any{
		"allowedProviders": []any{"openai", 7, ""}, "allowedModels": []any{"gpt-4o"}, "capture": true, "privacyMode": false,
	})
	if p == nil || len(p.AllowedProviders) != 1 || p.AllowedProviders[0] != "openai" || p.AllowedModels[0] != "gpt-4o" || !p.Capture || p.PrivacyMode == nil || *p.PrivacyMode {
		t.Fatalf("policy = %+v", p)
	}
}

// --- through a real tollgate proxy and the testkit fake upstream ---

type spy struct {
	tollgate.NoopHook
	mu    sync.Mutex
	calls []tollgate.Call
}

func (s *spy) Name() string { return "spy" }
func (s *spy) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, tollgate.Call{Tenant: c.Tenant, Meta: copyMeta(c.Meta), Path: c.Path})
	return nil, nil
}

func copyMeta(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// pathResolver routes /w/{id}/v1/messages to the anthropic wire and the rest to openai, both on
// the loopback fake; the path the provider sees is the part after /w/{id}.
type pathResolver struct{ base *url.URL }

func (r pathResolver) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	_, rest, _ := wsauth.Parse(req.URL.Path)
	if strings.HasSuffix(rest, "/messages") {
		return tollgate.Provider{Name: "anthropic", Wire: tollgate.WireAnthropicMessages, BaseURL: r.base}, rest, nil
	}
	return tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: r.base}, rest, nil
}

type rig struct {
	srv *httptest.Server
	up  *testkit.FakeUpstream
	spy *spy
}

func newRig(t *testing.T) *rig {
	t.Helper()
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	p, err := tollgate.New(tollgate.Options{
		Auth: wsauth.New(wsauth.Uncached((&dir{}).ByPublicID), 0), Providers: pathResolver{base}, Limits: tollgate.DefaultLimits(),
		Admission: tollgate.Unlimited{}, Metrics: tollgate.DiscardSink{},
	})
	if err != nil {
		t.Fatal(err)
	}
	sp := &spy{}
	p.Use(wsauth.Hook{}, sp)
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	return &rig{srv: srv, up: up, spy: sp}
}

func (r *rig) post(t *testing.T, path, fixture string, hdr map[string]string) (int, string) {
	t.Helper()
	_, body := testkit.RequestFor(fixture)
	req, err := http.NewRequestWithContext(context.Background(), "POST", r.srv.URL+path, strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testkit.FixtureHeader, fixture)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

var wires = []struct {
	name, fixture, path string
	creds               map[string]string
	credHeader          string
}{
	{"openai", "openai/chat-stream-text", "/v1/chat/completions", map[string]string{"Authorization": "Bearer sk-live-secret"}, "Authorization"},
	{"anthropic", "anthropic/messages-stream-tool-use", "/v1/messages", map[string]string{"x-api-key": "sk-ant-live-secret", "anthropic-version": "2023-06-01"}, "x-api-key"},
}

func TestBothWiresStreamThroughTheWorkspaceURLWithCredentialsUntouched(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			r := newRig(t)
			code, body := r.post(t, "/w/"+curID+w.path, w.fixture, w.creds)
			if code != 200 || !strings.Contains(body, "data:") && !strings.Contains(body, "event:") {
				t.Fatalf("status %d body %q", code, body)
			}
			reqs := r.up.Requests()
			if len(reqs) != 1 {
				t.Fatalf("upstream saw %d requests", len(reqs))
			}
			up := reqs[0]
			if up.Path != w.path {
				t.Fatalf("upstream path %q, want %q (the /w/{id} prefix must not reach the provider)", up.Path, w.path)
			}
			if got := up.Header.Get(w.credHeader); got != w.creds[w.credHeader] {
				t.Fatalf("%s reached the upstream as %q", w.credHeader, got)
			}
			for k, vs := range up.Header {
				for _, v := range vs {
					low := strings.ToLower(k + "=" + v)
					if strings.Contains(v, curID) || strings.Contains(low, "meterwise") || strings.Contains(low, "tenant") || strings.Contains(low, "workspace") || strings.Contains(low, "x-wsauth") {
						t.Errorf("a header of ours reached the upstream: %s: %s", k, v)
					}
				}
			}
			if len(r.spy.calls) != 1 || r.spy.calls[0].Tenant.ID != wsID || r.spy.calls[0].Tenant.Attrs["org_id"] != orgID {
				t.Fatalf("hook saw %+v", r.spy.calls)
			}
			if _, flagged := r.spy.calls[0].Meta[wsauth.MetaURLRetired]; flagged {
				t.Fatal("a current id was flagged as retired")
			}
		})
	}
}

func TestRetiredIDInGraceStreamsAndIsFlaggedInMeta(t *testing.T) {
	for _, w := range wires {
		t.Run(w.name, func(t *testing.T) {
			r := newRig(t)
			if code, _ := r.post(t, "/w/"+oldID+w.path, w.fixture, w.creds); code != 200 {
				t.Fatalf("status %d", code)
			}
			if len(r.spy.calls) != 1 || r.spy.calls[0].Meta[wsauth.MetaURLRetired] != "true" {
				t.Fatalf("meta = %+v", r.spy.calls)
			}
		})
	}
}

func TestRefusalsNeverReachTheUpstream(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		want     int
	}{
		{"unknown", strings.Repeat("A", 22), 404}, {"reset past grace", resetID, 410}, {"cidr mismatch", cidrID, 403},
	} {
		for _, w := range wires {
			t.Run(tc.name+"/"+w.name, func(t *testing.T) {
				r := newRig(t)
				code, body := r.post(t, "/w/"+tc.id+w.path, w.fixture, w.creds)
				if code != tc.want {
					t.Fatalf("status %d %q, want %d", code, body, tc.want)
				}
				for _, secret := range []string{"sk-live-secret", "sk-ant-live-secret", tc.id} {
					if strings.Contains(body, secret) {
						t.Fatalf("the refusal body leaks %q: %s", secret, body)
					}
				}
				if n := len(r.up.Requests()); n != 0 {
					t.Fatalf("upstream saw %d requests after a refusal", n)
				}
				if len(r.spy.calls) != 0 {
					t.Fatal("a hook ran for a refused call")
				}
			})
		}
	}
}
