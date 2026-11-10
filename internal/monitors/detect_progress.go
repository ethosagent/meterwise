package monitors

import (
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DetectNoProgress fires when the last two assistant turns made no write, the newer read nothing the
// older had not, and their texts are at least SimilarityPct percent alike (shingle Jaccard; equal
// text hashes when a turn is too short to have shingles).
func DetectNoProgress(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.NoProgress
	calls := r.Calls()
	n := len(calls)
	if n < 2 {
		return contract.MonitorVerdict{}, false
	}
	prev, cur := calls[n-2], calls[n-1]
	if hasWrite(prev) || hasWrite(cur) || newRead(prev, cur) {
		return contract.MonitorVerdict{}, false
	}
	sim, ok := similarity(prev, cur)
	if !ok || sim < k.SimilarityPct {
		return contract.MonitorVerdict{}, false
	}
	return newVerdict(contract.MonitorNoProgress, k.Mode, contract.MonitorEvidence{
		MatchedCalls: []int{prev.Index, cur.Index}, Hash: hashID(c, cur.TextHash), Count: sim, Threshold: k.SimilarityPct,
	}), true
}

func newRead(prev, cur contract.CallSummary) bool {
	for _, h := range cur.ReadHashes {
		if !slices.Contains(prev.ReadHashes, h) {
			return true
		}
	}
	return false
}

// similarity is the percentage two turns' texts agree and whether it could be measured.
func similarity(a, b contract.CallSummary) (int, bool) {
	if len(a.Shingles) > 0 && len(b.Shingles) > 0 {
		return jaccardPct(sortedSet(a.Shingles), sortedSet(b.Shingles)), true
	}
	if a.TextHash != 0 && a.TextHash == b.TextHash {
		return 100, true
	}
	return 0, false
}

// DetectStepBudget fires on the first call past MaxSteps and again on the first call past 1.5x
// MaxSteps (stateless: the session's call count is exact, so a restart changes nothing).
func DetectStepBudget(c *tollgate.Call, _ contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.StepBudget
	first, second := k.MaxSteps, k.MaxSteps+k.MaxSteps/2
	var thr int
	switch c.Session.Calls {
	case first + 1:
		thr = first
	case second + 1:
		thr = second
	default:
		return contract.MonitorVerdict{}, false
	}
	return newVerdict(contract.MonitorStepBudget, k.Mode, contract.MonitorEvidence{Count: c.Session.Calls, Threshold: thr}), true
}
