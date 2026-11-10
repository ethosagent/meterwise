package monitors_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// The rig is the real chain in the order the host wires it (ruling R2): history, monitors, then a
// stand-in for the ledger that seals the verdicts in After. The upstream is testkit's FakeUpstream.

type seen struct {
	index    int
	verdicts []contract.MonitorVerdict
	notes    int
	body     []byte
	status   int
	usage    tollgate.Usage
}

type tap struct {
	tollgate.NoopHook
	ch chan seen
}

func (*tap) Name() string { return "tap" }

func (p *tap) After(_ context.Context, c *tollgate.Call, m tollgate.Metric) {
	s := seen{index: c.Session.Calls, body: append([]byte(nil), c.Body...), status: m.Status, usage: m.Usage}
	if vs, ok := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts); ok {
		s.verdicts, s.notes = vs.Seal()
	}
	p.ch <- s
}

// swapHist lets a test replace the history hook mid-session, as a process restart would.
type swapHist struct {
	tollgate.NoopHook
	cur atomic.Pointer[history.Hook]
}

func (*swapHist) Name() string { return "history" }
func (s *swapHist) Before(ctx context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	return s.cur.Load().Before(ctx, c)
}
func (s *swapHist) OnEvent(ctx context.Context, c *tollgate.Call, e tollgate.Event) {
	s.cur.Load().OnEvent(ctx, c, e)
}
func (s *swapHist) After(ctx context.Context, c *tollgate.Call, m tollgate.Metric) {
	s.cur.Load().After(ctx, c, m)
}
func (s *swapHist) OnSessionClosed(ctx context.Context, ss tollgate.Session) {
	s.cur.Load().OnSessionClosed(ctx, ss)
}

type tenantAuth struct {
	attrs atomic.Pointer[map[string]string]
}

func (a *tenantAuth) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	t := tollgate.Tenant{ID: "ws"}
	if m := a.attrs.Load(); m != nil {
		t.Attrs = *m
	}
	return t, nil
}

type resolver struct{ base *url.URL }

func (r resolver) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	name, rest, _ := strings.Cut(strings.TrimPrefix(req.URL.Path, "/"), "/")
	wire := tollgate.WireOpenAIChat
	if name == "anthropic" {
		wire = tollgate.WireAnthropicMessages
	}
	return tollgate.Provider{Name: name, Wire: wire, BaseURL: r.base}, "/" + rest, nil
}

type sessions struct {
	mu    sync.Mutex
	calls map[string]*tollgate.Session
	out   chan tollgate.Session
}

func (s *sessions) Resolve(_ context.Context, t tollgate.Tenant, c *tollgate.Call) tollgate.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := t.ID + "\x00" + c.SessionHeader
	sess := s.calls[k]
	if sess == nil {
		sess = &tollgate.Session{ID: c.SessionHeader, TenantID: t.ID, Source: tollgate.SessionFromHeader, StartedAt: time.Now()}
		s.calls[k] = sess
	}
	sess.Calls++
	sess.LastSeen = time.Now()
	return *sess
}

func (s *sessions) Closed() <-chan tollgate.Session { return s.out }

type rig struct {
	t    testing.TB
	up   *testkit.FakeUpstream
	srv  *httptest.Server
	hist *swapHist
	mon  *monitors.Hook
	tap  *tap
	auth *tenantAuth
}

func newRig(t testing.TB, attrs map[string]string, opts monitors.Options) *rig {
	t.Helper()
	r := &rig{t: t, up: testkit.NewFakeUpstream(t, testkit.Options{}), hist: &swapHist{}, tap: &tap{ch: make(chan seen, 64)}, auth: &tenantAuth{}}
	r.hist.cur.Store(history.New(history.Options{}))
	r.setAttrs(attrs)
	r.mon = newMon(opts)
	base, _ := url.Parse(r.up.URL)
	p, err := tollgate.New(tollgate.Options{Auth: r.auth, Providers: resolver{base}, Sessions: &sessions{calls: map[string]*tollgate.Session{}, out: make(chan tollgate.Session)}})
	if err != nil {
		t.Fatal(err)
	}
	p.Use(r.hist, r.mon, r.tap)
	ctx, cancel := context.WithCancel(context.Background())
	if err := p.Start(ctx); err != nil {
		t.Fatal(err)
	}
	r.srv = httptest.NewServer(p.Handler())
	t.Cleanup(func() { r.srv.Close(); cancel(); _ = p.Close(context.Background()) })
	return r
}

func (r *rig) setAttrs(a map[string]string) { r.auth.attrs.Store(&a) }

type reply struct {
	status int
	header http.Header
	body   []byte
	sent   []byte
	seen   seen
}

// fixtureFor names the upstream response the fake replays for a wire and stream mode.
func fixtureFor(w tollgate.WireFormat, stream bool) string {
	switch {
	case w == tollgate.WireOpenAIChat && stream:
		return "openai/chat-stream-tools"
	case w == tollgate.WireOpenAIChat:
		return "openai/chat"
	case stream:
		return "anthropic/messages-stream-tool-use"
	}
	return "anthropic/messages"
}

// call sends one request of session "e2e" and waits until every hook has finished with it.
func (r *rig) call(w tollgate.WireFormat, stream bool, msgs []tollgate.Message) reply {
	r.t.Helper()
	body := requestBody(r.t, w, stream, msgs)
	path := "/openai/v1/chat/completions"
	if w == tollgate.WireAnthropicMessages {
		path = "/anthropic/v1/messages"
	}
	req, _ := http.NewRequest(http.MethodPost, r.srv.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-session-id", "e2e")
	req.Header.Set(testkit.FixtureHeader, fixtureFor(w, stream))
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	rep := reply{status: resp.StatusCode, header: resp.Header, body: b, sent: body}
	select {
	case rep.seen = <-r.tap.ch:
	case <-time.After(10 * time.Second):
		r.t.Fatal("the call was never accounted (After did not run)")
	}
	return rep
}

func (r *rig) upstreamBodies() [][]byte {
	var out [][]byte
	for _, q := range r.up.Requests() {
		out = append(out, q.Body)
	}
	return out
}

type historyHook = history.Hook

func newHistoryHook() *history.Hook { return history.New(history.Options{}) }
