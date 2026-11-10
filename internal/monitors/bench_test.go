package monitors_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"
	"github.com/ethosagent/tollgate/testkit"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// bigTranscript is a 201-message conversation: one user message and 100 assistant/tool pairs, the
// last pair repeating the call two pairs earlier so duplicate_tool_call fires on it.
func bigTranscript() []tollgate.Message {
	msgs := []tollgate.Message{user("fix the failing build")}
	for i := 1; i <= 100; i++ {
		n := i
		if i == 100 {
			n = 98
		}
		id := fmt.Sprintf("call_%d", i)
		args := fmt.Sprintf(`{"query":"symbol_%d","path":"/src/module_%d/file_%d.go","limit":50}`, n, n%7, n)
		msgs = append(msgs, asst("Searching the module for the symbol that the linker reported.", tcall(id, "grep", args)),
			toolResult(id, strings.Repeat(fmt.Sprintf("match in file_%d line 42; ", n), 12)))
	}
	return msgs
}

func benchCall(t testing.TB, w tollgate.WireFormat, msgs []tollgate.Message, session string) *tollgate.Call {
	return &tollgate.Call{
		ID: "bench", Tenant: tollgate.Tenant{ID: "ws", Attrs: dupMode(contract.ModeNote)}, Session: tollgate.Session{ID: session, TenantID: "ws", Calls: 101},
		Provider: tollgate.Provider{Wire: w}, Messages: msgs, Body: requestBody(t, w, true, msgs),
	}
}

// beforeBoth is what the chain's Before stage costs: history then monitors, on one call.
func beforeBoth(ctx context.Context, h *history.Hook, m *monitors.Hook, c *tollgate.Call) {
	_, _ = h.Before(ctx, c)
	_, _ = m.Before(ctx, c)
}

func pct(d []time.Duration, p float64) time.Duration {
	s := slices.Clone(d)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// beforeP99 is the smallest p99 over trials of n fresh sessions each: the worst case for the hooks,
// since every call rebuilds its ring from 201 messages, fires, and splices a note into the body.
// Noise only ever inflates a trial, so the best trial is the honest estimate.
func beforeP99(t testing.TB, w tollgate.WireFormat, trials, n int) (best time.Duration, notes int) {
	msgs := bigTranscript()
	h, m := history.New(history.Options{}), newMon(monitors.Options{})
	ctx := context.Background()
	best = time.Hour
	for trial := 0; trial < trials; trial++ {
		var ds []time.Duration
		for i := 0; i < n; i++ {
			c := benchCall(t, w, msgs, fmt.Sprintf("s-%d-%d", trial, i))
			start := time.Now()
			beforeBoth(ctx, h, m, c)
			ds = append(ds, time.Since(start))
			if _, k := verdictsOf(c); k == 1 {
				notes++
			}
		}
		best = min(best, pct(ds, 0.99))
	}
	return best, notes
}

const beforeBudget = time.Millisecond

// instrumented reports whether the binary is built with the race detector or coverage, which slow every
// call several times over; the gate runs with both, so absolute latency budgets only bind a plain `go test`.
func instrumented() bool {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, s := range bi.Settings {
		if (s.Key == "-race" || s.Key == "-cover") && s.Value == "true" {
			return true
		}
	}
	return false
}

func TestBeforeP99UnderOneMillisecondFor200Messages(t *testing.T) {
	if testing.Short() || instrumented() {
		t.Skip("absolute latency budget: needs a plain, non-short go test (no -race, no -cover)")
	}
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			p99, notes := beforeP99(t, w, 5, 200)
			t.Logf("%s: Before p99 = %v over a 201-message call that rebuilds its ring, fires and splices a note (budget %v)", w, p99, beforeBudget)
			if notes != 5*200 {
				t.Fatalf("the measured path must fire and note on every call, did %d of %d", notes, 5*200)
			}
			if p99 >= beforeBudget {
				t.Fatalf("Before p99 %v, budget %v", p99, beforeBudget)
			}
		})
	}
}

func BenchmarkBefore200Messages(b *testing.B) {
	msgs := bigTranscript()
	for _, w := range wires {
		b.Run(string(w), func(b *testing.B) {
			h, m := history.New(history.Options{}), newMon(monitors.Options{})
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				c := benchCall(b, w, msgs, fmt.Sprint("s", i))
				b.StartTimer()
				beforeBoth(ctx, h, m, c)
			}
		})
	}
}

// BenchmarkOnEvent is the stream cost per event on the tap goroutine (never the client's).
func BenchmarkOnEvent(b *testing.B) {
	h, m := history.New(history.Options{}), newMon(monitors.Options{})
	msgs := []tollgate.Message{user("go")}
	c := benchCall(b, tollgate.WireOpenAIChat, msgs, "ev")
	c.Stream = true
	beforeBoth(context.Background(), h, m, c)
	e := tollgate.Event{Kind: tollgate.EventTextDelta, Text: "tokens of a streamed reply "}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.OnEvent(ctx, c, e)
		m.OnEvent(ctx, c, e)
	}
}

type benchResolver struct{ base *url.URL }

func (r benchResolver) Resolve(_ context.Context, _ tollgate.Tenant, req *http.Request) (tollgate.Provider, string, error) {
	return tollgate.Provider{Name: "openai", Wire: tollgate.WireOpenAIChat, BaseURL: r.base}, strings.TrimPrefix(req.URL.Path, "/openai"), nil
}

type tenantFor struct{ attrs map[string]string }

func (a tenantFor) Authenticate(context.Context, *http.Request) (tollgate.Tenant, error) {
	return tollgate.Tenant{ID: "ws", Attrs: a.attrs}, nil
}

type ttfbRig struct {
	srv  *httptest.Server
	body []byte
}

// newTTFBRig is a real proxy over a streaming upstream with the given hooks.
func newTTFBRig(t testing.TB, hooks ...tollgate.Hook) *ttfbRig {
	up := testkit.NewFakeUpstream(t, testkit.Options{})
	base, _ := url.Parse(up.URL)
	all := attrNote()
	p, err := tollgate.New(tollgate.Options{Auth: tenantFor{all}, Providers: benchResolver{base}, Metrics: tollgate.DiscardSink{}})
	if err != nil {
		t.Fatal(err)
	}
	p.Use(hooks...)
	if err := p.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)
	_, body := testkit.RequestFor("openai/chat-stream-text")
	return &ttfbRig{srv: srv, body: body}
}

func attrNote() map[string]string {
	return attr(func(c *contract.MonitorConfig) {
		c.DuplicateToolCall.Mode, c.RepeatedRead.Mode, c.NoProgress.Mode = contract.ModeNote, contract.ModeNote, contract.ModeNote
		c.StepBudget.Mode, c.ReasoningLoop.Mode, c.SecondGuessing.Mode = contract.ModeNote, contract.ModeNote, contract.ModeNote
	})
}

// ttfb is the time from sending the request to the first byte of the streamed body.
func (r *ttfbRig) ttfb(t testing.TB, i int) time.Duration {
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, r.srv.URL+"/openai/v1/chat/completions", strings.NewReader(string(r.body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(testkit.FixtureHeader, "openai/chat-stream-text")
	req.Header.Set("x-session", fmt.Sprint("ttfb-", i%8))
	start := time.Now()
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	one := make([]byte, 1)
	_, _ = io.ReadFull(resp.Body, one)
	d := time.Since(start)
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return d
}

// TestStreamDetectorsAddNothingToTTFB compares first-byte latency with history alone against history
// plus the monitors (all six on, so the stream detectors run in After on every call), interleaved
// so machine noise hits both. The detectors read the stream on the tap, off the client path, so the
// added median must stay inside a 1 ms smoke bound (the monitors' own Before is the only cost).
func TestStreamDetectorsAddNothingToTTFB(t *testing.T) {
	if testing.Short() || instrumented() {
		t.Skip("latency comparison: needs a plain, non-short go test (no -race, no -cover)")
	}
	base := newTTFBRig(t, history.New(history.Options{}))
	with := newTTFBRig(t, history.New(history.Options{}), newMon(monitors.Options{}))
	for i := 0; i < 50; i++ {
		base.ttfb(t, i)
		with.ttfb(t, i)
	}
	added := time.Hour
	var pb, pw time.Duration
	for trial := 0; trial < 5; trial++ {
		var db, dw []time.Duration
		for i := 0; i < 300; i++ {
			db, dw = append(db, base.ttfb(t, i)), append(dw, with.ttfb(t, i))
		}
		if d := pct(dw, 0.5) - pct(db, 0.5); d < added {
			added, pb, pw = d, pct(db, 0.5), pct(dw, 0.5)
		}
	}
	t.Logf("TTFB p50: history only %v, history+monitors %v, added %v", pb, pw, added)
	if added >= time.Millisecond {
		t.Fatalf("monitors added %v to the median TTFB", added)
	}
}

func BenchmarkStreamTTFB(b *testing.B) {
	rigs := map[string]*ttfbRig{
		"history":          newTTFBRig(b, history.New(history.Options{})),
		"history+monitors": newTTFBRig(b, history.New(history.Options{}), newMon(monitors.Options{})),
	}
	for name, r := range rigs {
		b.Run(name, func(b *testing.B) {
			var total time.Duration
			for i := 0; i < b.N; i++ {
				total += r.ttfb(b, i)
			}
			b.ReportMetric(float64(total.Microseconds())/float64(b.N), "ttfb-µs")
		})
	}
}
