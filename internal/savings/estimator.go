package savings

import "github.com/ethosagent/meterwise/internal/contract"

// Estimate turns one monitor verdict into the tokens it saved (or, for an observe verdict, would
// save) and how sure that figure is. It returns tokens, not money: the ledger prices them with the
// Pricer at the call's start (ruling R6).
//
//   - duplicate_tool_call and repeated_read: the avoided call, Input = prior input tokens +
//     ceil(body growth bytes / 4), Output = the session's median output. Certain when the call was
//     blocked, or observed under a configured block mode (what blocking would have saved); a note
//     only hopes to avoid the repeat, so it is likely.
//   - no_progress: the same one avoided call, always likely.
//   - step_budget, reasoning_loop, second_guessing: nothing at fire time, confidence none.
//
// Whether the figure is counted in saved, likely saved or neither (observe is would-save) is the
// ledger's decision from the verdict's Action; the two numbers are never summed.
func Estimate(v contract.MonitorVerdict) (contract.Usage, contract.Confidence) {
	switch v.Monitor {
	case contract.MonitorDuplicateToolCall, contract.MonitorRepeatedRead, contract.MonitorNoProgress:
	default:
		return contract.Usage{}, contract.ConfidenceNone
	}
	b := v.Basis
	u := contract.Usage{
		Input:  max(b.PriorInputTokens, 0) + ceilDiv(max(b.BodyDeltaBytes, 0), 4),
		Output: max(b.MedianOutputTokens, 0),
	}
	if v.Monitor == contract.MonitorNoProgress {
		return u, contract.ConfidenceLikely
	}
	blocked := v.Action == contract.ModeBlock || (v.Action == contract.ModeObserve && v.Mode == contract.ModeBlock)
	if blocked {
		return u, contract.ConfidenceCertain
	}
	return u, contract.ConfidenceLikely
}

func ceilDiv(n, d int64) int64 { return (n + d - 1) / d }
