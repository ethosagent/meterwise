package monitors

import (
	"math/rand/v2"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/savings"
)

// A client that sends call n+1 before call n's After has run gets the previous entry caught up from the request
// (ADR 0024): Input, Output and BodyBytes unknown (zero). The block estimate priced from such a fill must be a
// lower bound, never above the exact estimate from the ledger's real numbers (I7; before the fix the whole body
// counted as growth: "blocked estimate 157, want 104").
func TestCaughtUpBlockEstimateIsNeverAboveTheExactOne(t *testing.T) {
	call := &tollgate.Call{Body: make([]byte, 4000), Session: tollgate.Session{Calls: 3}}
	exact := []contract.CallSummary{
		{Index: 1, Input: 100, Output: 20, BodyBytes: 1000, UsageKnown: true},
		{Index: 2, Input: 160, Output: 30, BodyBytes: 2000, UsageKnown: true},
	}
	filled := []contract.CallSummary{exact[0], {Index: 2}} // call 2 caught up: usage and body size unknown
	verdict := func(calls []contract.CallSummary) int64 {
		u, conf := savings.Estimate(contract.MonitorVerdict{
			Monitor: contract.MonitorDuplicateToolCall, Action: contract.ModeBlock, Mode: contract.ModeBlock, Basis: basisOf(call, calls),
		})
		if conf != contract.ConfidenceCertain {
			t.Fatalf("confidence = %v", conf)
		}
		return u.Input + u.Output
	}
	got, want := verdict(filled), verdict(exact)
	if got > want {
		t.Fatalf("caught-up estimate %d tokens exceeds the exact %d", got, want)
	}
	if b := basisOf(call, filled); b.BodyDeltaBytes != 0 || b.PriorInputTokens != 0 {
		t.Fatalf("a filled entry has no known size or usage: basis %+v", b)
	}
	if b := basisOf(call, exact); b.PriorInputTokens != 160 || b.BodyDeltaBytes != 2000 {
		t.Fatalf("exact basis changed: %+v", b)
	}
}

// Whatever mix of known and unknown usage a ring holds (a rebuild after a restart, a catch-up, an After that
// failed), the certain block estimate is a lower bound of the one priced from the real numbers (M2).
func TestBlockEstimateIsALowerBoundForAnyMixOfKnownAndUnknownEntries(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	estimate := func(call *tollgate.Call, calls []contract.CallSummary) int64 {
		u, _ := savings.Estimate(contract.MonitorVerdict{
			Monitor: contract.MonitorDuplicateToolCall, Action: contract.ModeBlock, Mode: contract.ModeBlock, Basis: basisOf(call, calls),
		})
		return u.Input + u.Output
	}
	for trial := 0; trial < 20000; trial++ {
		n := 1 + rng.IntN(8)
		exact := make([]contract.CallSummary, n)
		filled := make([]contract.CallSummary, n)
		for i := range exact {
			out := int64(rng.IntN(900))
			if rng.IntN(5) == 0 {
				out = 0 // a real call can have no output
			}
			exact[i] = contract.CallSummary{Index: i + 1, Input: int64(rng.IntN(5000)), Output: out, BodyBytes: rng.IntN(20000), UsageKnown: true}
			filled[i] = exact[i]
			if rng.IntN(2) == 0 {
				filled[i] = contract.CallSummary{Index: i + 1} // caught up: nothing known
			}
		}
		call := &tollgate.Call{Body: make([]byte, rng.IntN(40000)), Session: tollgate.Session{Calls: n + 1}}
		if got, want := estimate(call, filled), estimate(call, exact); got > want {
			t.Fatalf("trial %d: estimate %d from %+v exceeds the exact %d from %+v", trial, got, filled, want, exact)
		}
	}
}

func TestNoKnownEntryMeansNoOutputPart(t *testing.T) {
	call := &tollgate.Call{Body: make([]byte, 100), Session: tollgate.Session{Calls: 3}}
	b := basisOf(call, []contract.CallSummary{{Index: 1}, {Index: 2}})
	if b != (contract.EstBasis{}) {
		t.Fatalf("basis = %+v, want the zero basis", b)
	}
}
