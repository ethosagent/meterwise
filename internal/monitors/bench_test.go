package monitors_test

import (
	"context"
	"encoding/json"
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
	"github.com/ethosagent/meterwise/internal/nonet"
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

// beforeStats is one trial of fresh sessions: the Before cost of the hooks and, interleaved with it, the
// cost of parsing the same request body as JSON (the reference every proxy pays), so load and
// instrumentation hit both alike.
type beforeStats struct{ p50, p99, refP50, refP99 time.Duration }

// measureBefore runs trials of n calls, each rebuilding its ring from 201 messages, firing and
// splicing a note (the worst case for the hooks). Noise only ever inflates a trial, so it returns the
// trial with the smallest p99 as the honest estimate, and how many calls carried a note.
func measureBefore(t testing.TB, w tollgate.WireFormat, trials, n int) (best beforeStats, notes int) {
	msgs := bigTranscript()
	h, m := history.New(history.Options{}), newMon(monitors.Options{})
	ctx := context.Background()
	best.p99 = time.Hour
	for trial := 0; trial < trials; trial++ {
		var ds, refs []time.Duration
		for i := 0; i < n; i++ {
			c := benchCall(t, w, msgs, fmt.Sprintf("s-%d-%d", trial, i))
			start := time.Now()
			var doc any
			_ = json.Unmarshal(c.Body, &doc)
			refs = append(refs, time.Since(start))
			start = time.Now()
			beforeBoth(ctx, h, m, c)
			ds = append(ds, time.Since(start))
			if _, k := verdictsOf(c); k == 1 {
				notes++
			}
		}
		if s := (beforeStats{pct(ds, 0.5), pct(ds, 0.99), pct(refs, 0.5), pct(refs, 0.99)}); s.p99 < best.p99 {
			best = s
		}
	}
	return best, notes
}

// instrumented reports whether the binary is built with the race detector or coverage, which slow every
// call several times over; the gate runs with both, so its budgets are 10x the plain-`go test` ones
// (the 1 ms target binds there; the gate only catches a path that blocks or went quadratic).
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

// scaled returns d, or 10x d in an instrumented binary.
func scaled(d time.Duration) time.Duration {
	if instrumented() {
		return 10 * d
	}
	return d
}

func TestBeforeP99UnderOneMillisecondFor200Messages(t *testing.T) {
	nonet.SkipTiming(t)
	const beforeBudget = time.Millisecond // binds a plain go test on a quiet machine; see below
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			trials, n := 5, 200
			if instrumented() {
				trials, n = 2, 50
			}
			s, notes := measureBefore(t, w, trials, n)
			t.Logf("%s: Before p50 %v p99 %v; parsing the same body as JSON p50 %v p99 %v", w, s.p50, s.p99, s.refP50, s.refP99)
			if notes != trials*n {
				t.Fatalf("the measured path must fire and note on every call, did %d of %d", notes, trials*n)
			}
			if s.p50 > s.refP50 {
				t.Errorf("Before p50 %v exceeds the cost of parsing the body once (%v): the hooks went superlinear", s.p50, s.refP50)
			}
			if quiet := s.refP50 < beforeBudget; quiet && !instrumented() && s.p99 >= beforeBudget {
				t.Errorf("Before p99 %v, budget %v (the machine is quiet: parsing the body had p50 %v)", s.p99, beforeBudget, s.refP50)
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
	nonet.SkipTiming(t)
	base := newTTFBRig(t, history.New(history.Options{}))
	with := newTTFBRig(t, history.New(history.Options{}), newMon(monitors.Options{}))
	for i := 0; i < 50; i++ {
		base.ttfb(t, i)
		with.ttfb(t, i)
	}
	var pbs, pws []time.Duration // the p50 of each trial, per rig
	trials, n := 5, 300
	if instrumented() {
		trials, n = 2, 100
	}
	for trial := 0; trial < trials; trial++ {
		var db, dw []time.Duration
		for i := 0; i < n; i++ {
			db, dw = append(db, base.ttfb(t, i)), append(dw, with.ttfb(t, i))
		}
		pbs, pws = append(pbs, pct(db, 0.5)), append(pws, pct(dw, 0.5))
	}
	pb, pw, added := pct(pbs, 0.5), pct(pws, 0.5), addedMedian(pbs, pws)
	t.Logf("TTFB p50: history only %v, history+monitors %v, added %v", pb, pw, added)
	if added >= scaled(time.Millisecond) {
		t.Fatalf("monitors added %v to the median TTFB", added)
	}
}

// addedMedian is the cost the monitors add: the median of the per-trial p50s with them minus the same
// without. Taking each rig's median apart (not the best of the per-trial differences) keeps one trial
// whose base rig absorbed noise from shrinking the difference.
func addedMedian(base, with []time.Duration) time.Duration { return pct(with, 0.5) - pct(base, 0.5) }

func TestAddedMedianIsNotTheBestTrial(t *testing.T) {
	ms := time.Millisecond
	// the monitors cost 5 ms in four trials; in the fifth the base rig was slowed by noise (9 ms vs 8 ms)
	base := []time.Duration{3 * ms, 3 * ms, 3 * ms, 3 * ms, 9 * ms}
	with := []time.Duration{8 * ms, 8 * ms, 8 * ms, 8 * ms, 8 * ms}
	if got := addedMedian(base, with); got != 5*ms {
		t.Fatalf("added = %v, want 5ms (a min of per-trial differences would say -1ms)", got)
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
