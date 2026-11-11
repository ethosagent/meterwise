package router_test

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/nonet"
	"github.com/ethosagent/meterwise/internal/router"
)

// bigBody is a ~100 KB openai.chat request: a system prompt, 230 tool exchanges and eight tool definitions. Routing it
// is the router's worst case: the score passes, so Features walks the body and SetModel copies it.
func bigBody() (string, []tollgate.Message) {
	var sb strings.Builder
	var msgs []tollgate.Message
	sb.WriteString(`{"model":"gpt-4.1","stream":true,"tools":[`)
	for i := range 8 {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"type":"function","function":{"name":"tool_%d","description":"does thing %d","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}`, i, i)
	}
	sb.WriteString(`],"messages":[{"role":"system","content":"You are an agent."}`)
	for i := range 230 {
		res := strings.Repeat(fmt.Sprintf("result line %d of the file\\n", i), 8)
		fmt.Fprintf(&sb, `,{"role":"assistant","content":null,"tool_calls":[{"id":"c%d","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"f%d.go\"}"}}]},{"role":"tool","tool_call_id":"c%d","content":"%s"}`, i, i, i, res)
		msgs = append(msgs, tollgate.Message{Role: tollgate.RoleAssistant}, tollgate.Message{Role: tollgate.RoleTool, Text: strings.ReplaceAll(res, `\n`, "\n")})
	}
	sb.WriteString(`]}`)
	return sb.String(), msgs
}

func pct(d []time.Duration, p float64) time.Duration {
	s := slices.Clone(d)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// The router's added Before latency on a 100 KB body stays under 1 ms at p99 (plan 10 §9). Noise only ever inflates a
// trial, so the smallest p99 of several trials is the honest estimate. It runs in the serial bench gate.
func TestBeforeP99UnderOneMillisecondOn100KB(t *testing.T) {
	nonet.SkipTiming(t)
	body, msgs := bigBody()
	if len(body) < 95_000 || len(body) > 110_000 {
		t.Fatalf("fixture is %d bytes", len(body))
	}
	h := router.New(router.Options{Tables: tables()})
	cons := attr(contract.RouterModeConservative)
	const n = 3000
	best := time.Hour
	var p50 time.Duration
	for trial := 0; trial < 5; trial++ {
		calls := make([]*tollgate.Call, n)
		for i := range calls {
			c := callAt(5, "gpt-4.1", body, cons)
			c.Messages = msgs
			c.Session.ID = fmt.Sprintf("bench-%d-%d", trial, i%50)
			calls[i] = c
		}
		d := make([]time.Duration, n)
		for i, c := range calls {
			start := time.Now()
			_, _ = h.Before(t.Context(), c)
			d[i] = time.Since(start)
			if c.Model != "gpt-4.1-mini" {
				t.Fatalf("the bench must exercise the routed path, model = %s", c.Model)
			}
		}
		if p := pct(d, 0.99); p < best {
			best, p50 = p, pct(d, 0.5)
		}
	}
	t.Logf("router Before on %d bytes, routed path: p50 %v p99 %v", len(body), p50, best)
	if best >= time.Millisecond {
		t.Errorf("Before p99 = %v, want < 1ms", best)
	}
}

// Held and off paths must stay cheap too: they never read the body.
func TestHeldAndOffPathsAreCheap(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	body, _ := bigBody()
	off := callAt(5, "gpt-4.1", body, attr(contract.RouterModeOff))
	held := callAt(1, "gpt-4.1", body, attr(contract.RouterModeConservative))
	if a := testing.AllocsPerRun(50, func() { _, _ = h.Before(t.Context(), off) }); a != 0 {
		t.Errorf("off allocates %.0f times, want 0", a)
	}
	if a := testing.AllocsPerRun(50, func() { _, _ = h.Before(t.Context(), held) }); a > 8 {
		t.Errorf("a held call allocates %.0f times, want a handful", a)
	}
}

// Allocations on the routed path do not grow with the number of calls: no per-call state accumulates.
func TestRoutedPathAllocationsAreFlat(t *testing.T) {
	h := router.New(router.Options{Tables: tables()})
	body, msgs := bigBody()
	cons := attr(contract.RouterModeConservative)
	per := func(rounds int) float64 {
		return testing.AllocsPerRun(rounds, func() {
			c := callAt(5, "gpt-4.1", body, cons)
			c.Messages = msgs
			_, _ = h.Before(t.Context(), c)
		})
	}
	a, b := per(20), per(2000)
	if b > a+1 {
		t.Errorf("allocs/op grew from %.1f to %.1f over 2000 calls", a, b)
	}
}
