package trim_test

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/trim"
)

// bigLog is a build log of n lines with an error about every 150 lines; seed makes each log distinct.
func bigLog(seed, n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i%150 == 0 {
			fmt.Fprintf(&b, "src/m%d_%d.c:%d:5: error: expected ';' before '}' token\n", seed, i, i%90)
			continue
		}
		fmt.Fprintf(&b, "[%d/%d] CC src/m%d_%d.c\n", i, n, seed, i)
	}
	return b.String()
}

// hundredKB is a request of about 100 KB: eight 12 KB tool results (logs, so every stage has work) and a fresh ninth.
func hundredKB(seed int) []step {
	steps := make([]step, 9)
	for i := range steps {
		steps[i] = step{tool: "bash", args: fmt.Sprintf(`{"cmd":"make step%d"}`, i), result: bigLog(seed*100+i, 420)}
	}
	return steps
}

func pct(d []time.Duration, p float64) time.Duration {
	s := slices.Clone(d)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// warm runs the first n-1 steps of a fresh session through the hook, so the timed call decides one new region.
func warm(ctx context.Context, t testing.TB, h *trim.Hook, w tollgate.WireFormat, id string, steps []step) *session {
	s := newSession(t, h, w, contract.TrimModeLossless)
	s.id = id
	s.sendCtx(ctx, steps[:len(steps)-1])
	return s
}

// T7: Before costs at most 1 ms at p99 on a 100 KB request, when it decides one new 12 KB result and replays eight
// remembered decisions, and when it decides a single cold 100 KB result. The race-instrumented run has a looser
// bound (10x), as the monitors' bench does.
func TestBeforeP99UnderOneMillisecondOn100KB(t *testing.T) {
	nonet.SkipTiming(t)
	ctx := context.Background()
	budget := time.Millisecond
	if raceOn() {
		budget *= 10
	}
	for _, w := range wires {
		t.Run(string(w), func(t *testing.T) {
			h := trim.New(trim.Options{})
			var steady, cold []time.Duration
			for i := 0; i < 300; i++ {
				steps := hundredKB(i)
				s := warm(ctx, t, h, w, fmt.Sprint("steady", i), steps)
				body := requestBody(t, w, steps, -1, false)
				if i == 0 {
					t.Logf("%s: request %d bytes", w, len(body))
				}
				c := s.build(steps)
				start := time.Now()
				s.run(ctx, c)
				steady = append(steady, time.Since(start))
				if r, ok := result(c); !ok || r.BodyAfter >= r.BodyBefore/2 {
					t.Fatalf("the measured call must trim: %+v", r)
				}
				big := []step{{tool: "bash", args: `{"cmd":"make all"}`, result: bigLog(i, 3600)}}
				cs := newSession(t, h, w, contract.TrimModeLossless)
				cs.id = fmt.Sprint("cold", i)
				cc := cs.build(big)
				start = time.Now()
				cs.run(ctx, cc)
				cold = append(cold, time.Since(start))
			}
			t.Logf("%s: steady (1 new 12 KB result, 8 remembered, 100 KB request) p50 %v p99 %v; cold 100 KB result p50 %v p99 %v",
				w, pct(steady, 0.5), pct(steady, 0.99), pct(cold, 0.5), pct(cold, 0.99))
			if p := pct(steady, 0.99); p > budget {
				t.Errorf("steady p99 %v > %v", p, budget)
			}
			if p := pct(cold, 0.99); p > budget {
				t.Errorf("cold p99 %v > %v", p, budget)
			}
		})
	}
}

// T7: across 10,000 calls the hook's allocations per call do not grow and the heap it keeps stays bounded.
func TestAllocationsStayFlatOver10kCalls(t *testing.T) {
	h := trim.New(trim.Options{MaxSessions: 50})
	steps := hundredKB(1)
	bodies := make([]*session, 100)
	for i := range bodies {
		bodies[i] = newSession(t, h, tollgate.WireOpenAIChat, contract.TrimModeLossless)
		bodies[i].id = fmt.Sprint("s", i)
	}
	window := func(from, to int) (perCall float64) {
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		for n := from; n < to; n++ {
			s := bodies[n%len(bodies)]
			s.send(steps[:3+n%5])
		}
		runtime.ReadMemStats(&m1)
		return float64(m1.Mallocs-m0.Mallocs) / float64(to-from)
	}
	unit := 1000 // the race detector makes a call ten times dearer, so it measures a tenth of the calls
	if raceOn() {
		unit = 100
	}
	window(0, unit)
	early := window(unit, 2*unit)
	for n := 2 * unit; n < 9*unit; n += unit {
		window(n, n+unit)
	}
	late := window(9*unit, 10*unit)
	t.Logf("allocations per call (including building the request): window 2: %.0f, window 10: %.0f (%d calls in all)", early, late, 10*unit)
	if late > early*1.1 {
		t.Errorf("allocations per call grew from %.0f to %.0f", early, late)
	}
	st := h.Status()
	if st["memo_sessions"].(int) > 50 {
		t.Errorf("memo holds %v sessions, bound is 50", st["memo_sessions"])
	}
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	t.Logf("heap in use after 10k calls: %d KiB, memo %v bytes over %v sessions", m.HeapAlloc>>10, st["memo_bytes"], st["memo_sessions"])
	if st["memo_bytes"].(int64) > 64<<20 {
		t.Errorf("memo bytes %v exceed the bound", st["memo_bytes"])
	}
}

func BenchmarkBefore100KB(b *testing.B) {
	for _, w := range wires {
		b.Run(string(w), func(b *testing.B) {
			h := trim.New(trim.Options{})
			steps := hundredKB(7)
			s := newSession(b, h, w, contract.TrimModeLossless)
			s.send(steps)
			ctx := context.Background()
			b.ReportAllocs()
			b.SetBytes(int64(len(requestBody(b, w, steps, -1, false))))
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				c := s.build(steps)
				b.StartTimer()
				s.run(ctx, c)
			}
		})
	}
}
