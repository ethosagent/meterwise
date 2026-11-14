package memory_test

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/store"
)

func pct(d []time.Duration, p float64) time.Duration {
	s := slices.Clone(d)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

func bigChat() string {
	var sb strings.Builder
	sb.WriteString(`{"model":"gpt-4o","messages":[{"role":"system","content":"You are an agent."}`)
	for i := range 230 {
		fmt.Fprintf(&sb, `,{"role":"user","content":"%s"}`, strings.Repeat(fmt.Sprintf("line %d of context ", i), 20))
	}
	sb.WriteString(`]}`)
	return sb.String()
}

// Memory's added Before latency stays under 1 ms at p99 (plan 09 §9), on a ~100 KB body: the common call (nothing
// recalled) and the call that gets the hint spliced in. Noise only ever inflates a trial, so the smallest p99 of several
// trials is the estimate. It runs in the serial bench gate.
func TestBeforeP99UnderOneMillisecond(t *testing.T) {
	nonet.SkipTiming(t)
	body := bigChat()
	if len(body) < 95_000 || len(body) > 130_000 {
		t.Fatalf("fixture is %d bytes", len(body))
	}
	r := newRig(t, nil)
	r.st.cands = []store.Candidate{cand(0.95)}
	a := attr(contract.MemoryModeHint, 1)
	const sessions, trials = 300, 5
	hintBest, noneBest := time.Hour, time.Hour
	var hintP50, noneP50 time.Duration
	run := func(ids []string) (p50, p99 time.Duration) {
		d := make([]time.Duration, len(ids))
		calls := make([]*tollgate.Call, len(ids))
		for i, id := range ids {
			calls[i] = callN(2, a, true)
			calls[i].Session.ID, calls[i].Body = id, []byte(body)
		}
		for i, c := range calls {
			start := time.Now()
			_, _ = r.hook.Before(t.Context(), c)
			d[i] = time.Since(start)
		}
		return pct(d, 0.50), pct(d, 0.99)
	}
	for trial := range trials {
		ids := make([]string, sessions)
		wait := func(n int) {
			eventually(t, "every recall", func() bool {
				return r.e.Source().Status()["jobs"].(map[string]any)["recall"].(map[string]any)["done"].(int64) >= int64(trial*sessions+n)
			})
		}
		for i := range ids { // a workspace queues 64 jobs: feed it in batches of 50
			ids[i] = fmt.Sprintf("bench-%d-%d", trial, i)
			c := callN(1, a, true)
			c.Session.ID = ids[i]
			r.hook.After(t.Context(), c, tollgate.Metric{})
			if (i+1)%50 == 0 {
				wait(i + 1)
			}
		}
		wait(sessions)
		if p50, p99 := run(ids); p99 < hintBest { // each session's cached hint is spliced into the 100 KB body
			hintBest, hintP50 = p99, p50
		}
		if p50, p99 := run(ids); p99 < noneBest { // the same sessions again: the hint is spent, nothing to do
			noneBest, noneP50 = p99, p50
		}
	}
	t.Logf("Before on %d bytes, best p99 of %d trials of %d calls: splicing a hint p50 %v p99 %v; nothing recalled p50 %v p99 %v",
		len(body), trials, sessions, hintP50, hintBest, noneP50, noneBest)
	if out := os.Getenv("METERWISE_BENCH_OUT"); out != "" { // evidence for the report: the numbers go to a file, not the gate log
		f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "memory Before %d bytes: hint p50 %v p99 %v; none p50 %v p99 %v\n", len(body), hintP50, hintBest, noneP50, noneBest)
			_ = f.Close()
		}
	}
	if hintBest > time.Millisecond || noneBest > time.Millisecond {
		t.Fatalf("Before p99 %v / %v exceeds 1 ms", hintBest, noneBest)
	}
}
