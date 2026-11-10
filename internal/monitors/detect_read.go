package monitors

import (
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// DetectRepeatedRead fires when the file read whose result is pending was read at least MinReads
// times in total, counting itself, with no write since the first of them. Evidence holds the path's
// hash; the Hook adds the plain path only when capture is allowed (ruling R7).
func DetectRepeatedRead(c *tollgate.Call, r contract.RingView, cfg contract.MonitorConfig) (contract.MonitorVerdict, bool) {
	k := cfg.RepeatedRead
	calls := r.Calls()
	n := len(calls)
	if n == 0 {
		return contract.MonitorVerdict{}, false
	}
	cur := calls[n-1]
	for _, p := range cur.ReadHashes {
		var matched []int
		for i := n - 2; i >= 0; i-- {
			if hasWrite(calls[i]) { // its write runs after its results: it lies between that turn and the pending call
				break
			}
			if slices.Contains(calls[i].ReadHashes, p) {
				matched = append(matched, calls[i].Index)
			}
		}
		if 1+len(matched) < k.MinReads {
			continue
		}
		slices.Reverse(matched)
		return newVerdict(contract.MonitorRepeatedRead, k.Mode, contract.MonitorEvidence{
			MatchedCalls: append(matched, cur.Index), Tool: firstReadTool(cur), PathHash: hashID(c, p),
			Count: 1 + len(matched), Threshold: k.MinReads,
		}), true
	}
	return contract.MonitorVerdict{}, false
}

func firstReadTool(s contract.CallSummary) string {
	for _, t := range s.Tools {
		if contract.ClassifyTool(t) == contract.ToolRead {
			return t
		}
	}
	return ""
}
