package monitors

import (
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
	ri, matched, ok := repeatScan(calls, n, k.MinReads, func(s contract.CallSummary) []uint64 { return s.ReadHashes }, readAt)
	if !ok {
		return contract.MonitorVerdict{}, false
	}
	return newVerdict(contract.MonitorRepeatedRead, k.Mode, contract.MonitorEvidence{
		MatchedCalls: append(matched, cur.Index), Tool: firstReadTool(cur), PathHash: hashID(c, cur.ReadHashes[ri]),
		Count: 1 + len(matched), Threshold: k.MinReads,
	}), true
}

func firstReadTool(s contract.CallSummary) string {
	for _, t := range s.Tools {
		if contract.ClassifyTool(t) == contract.ToolRead {
			return t
		}
	}
	return ""
}
