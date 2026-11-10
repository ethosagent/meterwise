package monitors

import (
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DetectDuplicateToolCall fires when the tool call whose result is pending (the newest ring entry's
// call) was made at least MaxRepeats times in total, counting itself, within the last LookBack earlier
// calls and with no write in between. With ABAB set it also fires on an A-B-A-B alternation, and with
// FuzzyPct > 0 on arguments whose token Jaccard similarity reaches that percentage.
func DetectDuplicateToolCall(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.DuplicateToolCall
	calls := r.Calls()
	if len(calls) == 0 || len(calls[len(calls)-1].ArgsHashes) == 0 {
		return contract.MonitorVerdict{}, false
	}
	if ev, ok := exactDuplicate(c, calls, k); ok {
		return newVerdict(contract.MonitorDuplicateToolCall, k.Mode, ev), true
	}
	if k.ABAB {
		if ev, ok := alternation(c, calls, k); ok {
			return newVerdict(contract.MonitorDuplicateToolCall, k.Mode, ev), true
		}
	}
	if k.FuzzyPct > 0 {
		if ev, ok := fuzzyDuplicate(c, calls, k); ok {
			return newVerdict(contract.MonitorDuplicateToolCall, k.Mode, ev), true
		}
	}
	return contract.MonitorVerdict{}, false
}

func exactDuplicate(c *tollgate.Call, calls []contract.CallSummary, k contract.DuplicateToolCallConfig) (contract.MonitorEvidence, bool) {
	n := len(calls)
	cur := calls[n-1]
	start := max(0, n-1-k.LookBack)
	for ti, h := range cur.ArgsHashes {
		var matched []int
		for i := n - 2; i >= start; i-- {
			if hasWrite(calls[i]) { // its write runs after its results: it lies between that turn and the pending call
				break
			}
			if slices.Contains(calls[i].ArgsHashes, h) {
				matched = append(matched, calls[i].Index)
			}
		}
		if 1+len(matched) < k.MaxRepeats {
			continue
		}
		slices.Reverse(matched)
		return contract.MonitorEvidence{
			MatchedCalls: append(matched, cur.Index), Tool: toolAt(cur, ti), ArgsHash: hashID(c, h),
			Count: 1 + len(matched), Threshold: k.MaxRepeats,
		}, true
	}
	return contract.MonitorEvidence{}, false
}

func toolAt(s contract.CallSummary, i int) string {
	if i < len(s.Tools) {
		return s.Tools[i]
	}
	return ""
}

// alternation finds A-B-A-B: the last four calls repeat in pairs, A differs from B, no write inside.
func alternation(c *tollgate.Call, calls []contract.CallSummary, _ contract.DuplicateToolCallConfig) (contract.MonitorEvidence, bool) {
	n := len(calls)
	if n < 4 {
		return contract.MonitorEvidence{}, false
	}
	a, b, a2, b2 := calls[n-4], calls[n-3], calls[n-2], calls[n-1]
	if !slices.Equal(a.ArgsHashes, a2.ArgsHashes) || !slices.Equal(b.ArgsHashes, b2.ArgsHashes) ||
		slices.Equal(a.ArgsHashes, b.ArgsHashes) || len(b2.ArgsHashes) == 0 || len(a.ArgsHashes) == 0 || hasWrite(a) || hasWrite(b) || hasWrite(a2) {
		return contract.MonitorEvidence{}, false
	}
	return contract.MonitorEvidence{
		MatchedCalls: []int{a.Index, b.Index, a2.Index, b2.Index}, Tool: toolAt(b2, 0),
		ArgsHash: hashID(c, b2.ArgsHashes[0]), Count: 2, Threshold: 2,
	}, true
}
