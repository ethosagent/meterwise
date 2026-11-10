package monitors

import (
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
	call := &tollgate.Call{Body: make([]byte, 4000)}
	exact := []contract.CallSummary{
		{Index: 1, Input: 100, Output: 20, BodyBytes: 1000},
		{Index: 2, Input: 160, Output: 30, BodyBytes: 2000},
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
