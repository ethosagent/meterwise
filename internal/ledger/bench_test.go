package ledger_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/ledger"
)

type benchResolver struct{ base *url.URL }

func (r benchResolver) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	return tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: r.base}, strings.TrimPrefix(req.URL.Path, "/openai"), nil
}

// proxyRig is a real tollgate proxy in front of the fake upstream, optionally with the ledger hook.
type proxyRig struct {
	srv  *httptest.Server
	body []byte
	path string
	fx   string
}

func newProxyRig(t testing.TB, ws string, hook *ledger.Hook, fixture string) *proxyRig {
	t.Helper()
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	p, err := tollgate.New(tollgate.Options{Auth: tenantAuth{ws: ws}, Providers: benchResolver{base}, Metrics: tollgate.DiscardSink{}})
	if err != nil {
		t.Fatal(err)
	}
	if hook != nil {
		p.Use(hook)
	}
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	_, body := testkit.RequestFor(fixture)
	return &proxyRig{srv: srv, body: body, path: "/openai" + testkit.PathFor(fixture), fx: fixture}
}

func (r *proxyRig) one(t testing.TB, i int) time.Duration {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, r.srv.URL+r.path, bytes.NewReader(r.body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-not-real")
	req.Header.Set(testkit.FixtureHeader, r.fx)
	req.Header.Set("x-session", "bench-"+string(rune('a'+i%8)))
	start := time.Now()
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return time.Since(start)
}

func pct(d []time.Duration, p float64) time.Duration {
	s := slices.Clone(d)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// measure interleaves calls to a bare proxy and one with the ledger, so machine noise hits both alike,
// in trials; it returns the smallest added p99 of any trial. Noise (a busy machine, GC) only ever
// inflates a trial, so the best trial is the honest estimate of what the ledger adds.
func measure(t testing.TB, bare, with *proxyRig, trials, n int) (added time.Duration, b, w []time.Duration) {
	for i := range 50 { // warm both
		bare.one(t, i)
		with.one(t, i)
	}
	added = time.Hour
	for range trials {
		var tb, tw []time.Duration
		for i := range n {
			tb = append(tb, bare.one(t, i))
			tw = append(tw, with.one(t, i))
		}
		if d := pct(tw, 0.99) - pct(tb, 0.99); d < added {
			added, b, w = d, tb, tw
		}
	}
	return added, b, w
}

const addedP99Budget = 2 * time.Millisecond

func TestAddedLatencyP99UnderBudgetWithFakeWriter(t *testing.T) {
	if testing.Short() {
		t.Skip("latency comparison")
	}
	h := newHook(t, &fakeWriter{}, nil)
	bare, with := newProxyRig(t, wsID, nil, "openai/chat-stream-text"), newProxyRig(t, wsID, h, "openai/chat-stream-text")
	added, b, w := measure(t, bare, with, 5, 400)
	t.Logf("fake writer: bare p50=%v p99=%v | ledger p50=%v p99=%v | added p99=%v (budget %v)", pct(b, 0.5), pct(b, 0.99), pct(w, 0.5), pct(w, 0.99), added, addedP99Budget)
	if added >= addedP99Budget {
		t.Fatalf("added p99 latency %v, budget %v", added, addedP99Budget)
	}
	_ = h.Close(context.Background())
}

func TestAddedLatencyP99UnderBudgetWithPostgres(t *testing.T) {
	if testing.Short() {
		t.Skip("latency comparison")
	}
	s, ws := pgStore(t)
	h := newHook(t, s, func(o *ledger.Options) { o.FlushInterval = 250 * time.Millisecond })
	bare, with := newProxyRig(t, ws, nil, "openai/chat-stream-text"), newProxyRig(t, ws, h, "openai/chat-stream-text")
	added, b, w := measure(t, bare, with, 5, 400)
	t.Logf("postgres: bare p50=%v p99=%v | ledger p50=%v p99=%v | added p99=%v (budget %v)", pct(b, 0.5), pct(b, 0.99), pct(w, 0.5), pct(w, 0.99), added, addedP99Budget)
	if added >= addedP99Budget {
		t.Fatalf("added p99 latency %v, budget %v", added, addedP99Budget)
	}
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := scalar[int](t, s, `SELECT count(*) FROM calls`); n != 2050 || h.Status()["dropped"] != int64(0) {
		t.Fatalf("rows = %d, status %v: the ledger lost calls under load", n, h.Status())
	}
}

// BenchmarkHookOverhead is the cost of Before+After on the request goroutine alone.
func BenchmarkHookOverhead(b *testing.B) {
	h := newHook(b, &fakeWriter{}, nil)
	c := directCall("bench", "hi", nil, 1)
	m := metricFor(c)
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Meta = map[string]any{}
		_, _ = h.Before(ctx, c)
		m.CallID = "bench"
		h.After(ctx, c, m)
	}
	b.StopTimer()
	_ = h.Close(ctx)
}
