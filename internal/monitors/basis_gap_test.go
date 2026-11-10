package monitors

import (
	"context"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
	"github.com/ethosagent/meterwise/internal/savings"
)

func certainEstimate(c *tollgate.Call, calls []contract.CallSummary) int64 {
	u, _ := savings.Estimate(contract.MonitorVerdict{
		Monitor: contract.MonitorDuplicateToolCall, Action: contract.ModeBlock, Mode: contract.ModeBlock, Basis: basisOf(c, calls),
	})
	return u.Input + u.Output
}

// growingRing is a realistic session of m finished calls: the input grows by the earlier output plus new
// content, and the body by 2 to 12 bytes per token. It returns the ring and the body size after the last call.
func growingRing(rng *rand.Rand, m int) ([]contract.CallSummary, int) {
	ring := make([]contract.CallSummary, m)
	in, body := int64(0), 0
	for k := range ring {
		out, add, bpt := int64(1+rng.IntN(800)), int64(50+rng.IntN(2000)), 2+rng.IntN(11)
		in += add
		body += int(add) * bpt
		ring[k] = contract.CallSummary{Index: k + 1, Input: in, Output: out, BodyBytes: body, UsageKnown: true}
		in += out
		body += int(out) * bpt
	}
	return ring, body
}

// degrade is the ring call n sees when only the first `present` of an After arrival order had run: the rest
// are absent (a catch-up the deadline skipped, a request not aligned) or unknown (caught up from the
// request); some of the present ones report no usage (zero input and output, flagged known as before R1-M1).
func degrade(rng *rand.Rand, exact []contract.CallSummary) []contract.CallSummary {
	order := rng.Perm(len(exact)) // After arrival order, shuffled
	present := make(map[int]bool)
	for _, k := range order[:rng.IntN(len(exact)+1)] {
		present[k] = true
	}
	catchUp := rng.IntN(2) == 0 // else the catch-up is skipped by its deadline: absent, not unknown
	var out []contract.CallSummary
	for k, s := range exact {
		switch {
		case present[k] && rng.IntN(6) == 0:
			out = append(out, contract.CallSummary{Index: s.Index, UsageKnown: true, BodyBytes: s.BodyBytes})
		case present[k]:
			out = append(out, s)
		case catchUp || rng.IntN(3) == 0:
			out = append(out, contract.CallSummary{Index: s.Index})
		}
	}
	return out
}

// The certain block estimate never exceeds the one priced from the real numbers, over rings with gaps,
// unknown entries, entries with no reported usage and an arbitrary After order (R1-M1, R1-M2).
func TestBlockEstimateIsALowerBoundOverGapsZeroUsageAndShuffledAfters(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 7))
	const trials = 120000
	over, worst := 0, int64(0)
	for i := 0; i < trials; i++ {
		m := 2 + rng.IntN(13)
		if rng.IntN(5) == 0 {
			m = contract.RingDepth + 1 + rng.IntN(30) // beyond the window: only the last RingDepth calls count
		}
		exact, body := growingRing(rng, m)
		call := &tollgate.Call{Body: make([]byte, body+rng.IntN(8000)), Session: tollgate.Session{Calls: len(exact) + 1}}
		if got, want := certainEstimate(call, degrade(rng, exact)), certainEstimate(call, exact); got > want {
			over++
			worst = max(worst, got-want)
		}
	}
	t.Logf("%d of %d rings overstate; worst excess %d tokens", over, trials, worst)
	if over > 0 {
		t.Fatalf("%d of %d rings overstate", over, trials)
	}
}

// R1-M2 (Opus probe): index n-1 absent, entry n-2 known. Pricing from n-2 would use its prior input and the body
// growth since n-2; the delta is unknown and the output part is 0 (an absent turn may hold any output).
func TestAbsentPreviousEntryGivesNoInputPartAndNoOutputPart(t *testing.T) {
	exact := []contract.CallSummary{
		{Index: 1, Input: 100, Output: 5, BodyBytes: 1000, UsageKnown: true},
		{Index: 2, Input: 200, Output: 10, BodyBytes: 2000, UsageKnown: true},
		{Index: 3, Input: 300, Output: 1000, BodyBytes: 3000, UsageKnown: true},
	}
	call := &tollgate.Call{Body: make([]byte, 9000), Session: tollgate.Session{Calls: 4}}
	gap := exact[:2] // call 3 never made it into the ring
	if b := basisOf(call, gap); b.PriorInputTokens != 0 || b.BodyDeltaBytes != 0 {
		t.Fatalf("with index 3 absent the basis must not price from entry 2: %+v", b)
	}
	if got, want := certainEstimate(call, gap), certainEstimate(call, exact); got > want {
		t.Fatalf("estimate %d exceeds the exact %d", got, want)
	}
}

// R1-M1 (Opus probe): an older entry flagged known with zero usage must not count as a known zero output.
func TestZeroUsageEntryCountsAsUnknown(t *testing.T) {
	exact := []contract.CallSummary{
		{Index: 1, Input: 100, Output: 5, BodyBytes: 1000, UsageKnown: true},
		{Index: 2, Input: 200, Output: 10, BodyBytes: 2000, UsageKnown: true},
		{Index: 3, Input: 300, Output: 1000, BodyBytes: 3000, UsageKnown: true},
	}
	filled := slices.Clone(exact)
	filled[0].Input, filled[0].Output = 0, 0
	call := &tollgate.Call{Body: make([]byte, 3400), Session: tollgate.Session{Calls: 4}}
	if got, want := certainEstimate(call, filled), certainEstimate(call, exact); got > want {
		t.Fatalf("estimate %d exceeds the exact %d", got, want)
	}
}

// R1-M2 end to end (Opus probe): a catch-up the deadline skips leaves index n-1 absent from the real history
// ring; the estimate priced from it stays below the one priced from the numbers each call reported.
func TestDeadlineSkippedCatchUpEstimateIsALowerBound(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	h := history.New(history.Options{Now: func() time.Time { return now }})
	call := func(n int, msgs ...tollgate.Message) *tollgate.Call {
		return &tollgate.Call{
			ID: "c", Tenant: tollgate.Tenant{ID: "ws"}, Session: tollgate.Session{ID: "s", Calls: n},
			Provider: tollgate.Provider{Wire: tollgate.WireOpenAIChat}, Messages: msgs, Body: make([]byte, 600*n),
		}
	}
	asst := func(s string) tollgate.Message { return tollgate.Message{Role: tollgate.RoleAssistant, Text: s} }
	play := func(c *tollgate.Call, in, out int64, say string) {
		t.Helper()
		if _, err := h.Before(context.Background(), c); err != nil {
			t.Fatal(err)
		}
		h.OnEvent(context.Background(), c, tollgate.Event{Kind: tollgate.EventTextDelta, Text: say})
		h.After(context.Background(), c, tollgate.Metric{Finish: tollgate.FinishOK, Status: 200, Usage: tollgate.Usage{InputTokens: in, OutputTokens: out}})
	}
	play(call(1), 100, 10, "one")
	usr := func() tollgate.Message { return tollgate.Message{Role: tollgate.RoleUser, Text: "next"} }
	play(call(2, asst("one"), usr()), 200, 20, "two")
	ctx, cancel := context.WithDeadline(context.Background(), now.Add(time.Millisecond))
	defer cancel()
	c4 := call(4, asst("one"), asst("two"), asst("three"), usr())
	if _, err := h.Before(ctx, c4); err != nil {
		t.Fatal(err)
	}
	ring := c4.Meta[contract.MetaRing].(contract.RingView).Calls()
	exact := []contract.CallSummary{
		{Index: 1, Input: 100, Output: 10, BodyBytes: 600, UsageKnown: true},
		{Index: 2, Input: 200, Output: 20, BodyBytes: 1200, UsageKnown: true},
		{Index: 3, Input: 250, Output: 10, BodyBytes: 1800, UsageKnown: true},
	}
	// The trailing user turn keeps the request aligned, so only the deadline can leave index 3 out of the ring.
	if idx := ringIndexes(ring); !slices.Equal(idx, []int{1, 2}) {
		t.Fatalf("ring indexes %v, want [1 2]", idx)
	}
	// The deadline ends aligned()'s comparison ("what cannot be compared is not aligned"), so the same skip also
	// counts once as heal_misaligned. caught_up stays 0: no catch-up ran.
	if st := h.Status(); st["skipped_deadline"] != int64(1) || st["heal_misaligned"] != int64(1) || st["caught_up"] != int64(0) {
		t.Fatalf("status %+v, want skipped_deadline 1, heal_misaligned 1, caught_up 0", st)
	}
	if b, want := basisOf(c4, ring), (contract.EstBasis{MedianOutputTokens: 10}); b != want {
		t.Fatalf("basis %+v, want %+v", b, want)
	}
	if got, want := certainEstimate(c4, ring), certainEstimate(c4, exact); got > want {
		t.Fatalf("estimate %d from ring %+v exceeds the exact %d", got, ring, want)
	}
}

func ringIndexes(ring []contract.CallSummary) []int {
	idx := make([]int, len(ring))
	for i, s := range ring {
		idx[i] = s.Index
	}
	return idx
}

// The output floor looks back at most RingDepth calls: lo = max(1, n-RingDepth). Calls older than that
// window are ignored, so a long session's early large outputs do not move the floor (a window of lo := 1
// would count them, they outnumber the window, and the floor would be 900, not 7).
func TestOutputFloorWindowIsTheLastRingDepthCalls(t *testing.T) {
	n := contract.RingDepth + 60 // 59 calls fall outside the window, more than the 50 inside it
	ring := make([]contract.CallSummary, 0, n-1)
	for k := 1; k < n; k++ {
		out := int64(7)
		if k < n-contract.RingDepth {
			out = 900 // outside the window [n-RingDepth, n-1]
		}
		ring = append(ring, contract.CallSummary{Index: k, Input: int64(100 * k), Output: out, BodyBytes: 10 * k, UsageKnown: true})
	}
	call := &tollgate.Call{Body: make([]byte, 20*n), Session: tollgate.Session{Calls: n}}
	if got := outputFloor(ring, n); got != 7 {
		t.Fatalf("floor %d, want 7 (the window is the last %d calls)", got, contract.RingDepth)
	}
	if b := basisOf(call, ring); b.MedianOutputTokens != 7 || b.PriorInputTokens != int64(100*(n-1)) {
		t.Fatalf("basis %+v", b)
	}
}
