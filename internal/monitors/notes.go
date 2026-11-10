package monitors

import (
	"fmt"

	"github.com/ethosagent/meterwise/internal/contract"
)

// notePrefix starts every text this package injects.
const notePrefix = "[proxy] "

// noteText is the fixed-template note for a verdict. It names the monitor's own facts (tool, counts,
// call numbers) and never repeats anything the model or the user wrote (ruling R7).
func noteText(v contract.MonitorVerdict) string {
	e := v.Evidence
	switch v.Monitor {
	case contract.MonitorDuplicateToolCall:
		return fmt.Sprintf("%sThe call to %q has now been made %d times in this session with the same arguments (calls %v). Reuse the earlier result instead of calling it again.",
			notePrefix, e.Tool, e.Count, e.MatchedCalls)
	case contract.MonitorRepeatedRead:
		return fmt.Sprintf("%sThe same file has been read %d times in this session with no change in between (calls %v). Work from what you already read.",
			notePrefix, e.Count, e.MatchedCalls)
	case contract.MonitorNoProgress:
		return fmt.Sprintf("%sThe last two steps look alike (%d%% similar) and changed nothing. Try a different approach or report what blocks you.",
			notePrefix, e.Count)
	case contract.MonitorStepBudget:
		return fmt.Sprintf("%sThis session is at step %d, past its budget of %d steps. Wrap up, or say what is still missing.",
			notePrefix, e.Count, e.Threshold)
	case contract.MonitorReasoningLoop:
		return fmt.Sprintf("%sYour last response repeated about %d tokens of its own reasoning. Decide and act instead of re-deriving.",
			notePrefix, e.Count)
	case contract.MonitorSecondGuessing:
		return fmt.Sprintf("%sYour last response reversed itself %d times and went back to a tool you had just left. Commit to one plan.",
			notePrefix, e.Count)
	}
	return notePrefix + "monitor note"
}

// blockText is the whole answer of a blocked call: a [proxy] note that names the monitor's own facts and
// where the earlier identical result is in the conversation (the tool call id that received it, a value
// the client chose and already holds). It repeats nothing the model or the user wrote (ruling R7).
func blockText(v contract.MonitorVerdict, loc resultLoc) string {
	what := "this call"
	if v.Evidence.Tool != "" {
		what = fmt.Sprintf("the call to %q", v.Evidence.Tool)
	}
	return fmt.Sprintf("%sBlocked by the %s monitor: %s repeats an earlier call (seen %d times, calls %v) and got the same result. "+
		"That result is already in this conversation, in the tool result for call id %q. The model was not called. "+
		"Use that result to continue, or change the arguments if you need something different.",
		notePrefix, v.Monitor, what, v.Evidence.Count, v.Evidence.MatchedCalls, loc.earlierID)
}

// blockKey identifies a block's subject per session: one block per session and subject (B1).
func blockKey(v contract.MonitorVerdict) string {
	return "block|" + v.Evidence.ArgsHash + "|" + v.Evidence.PathHash
}

// dedupKey identifies what a note is about, per session: the monitor plus its subject.
func dedupKey(v contract.MonitorVerdict) string {
	e := v.Evidence
	return fmt.Sprintf("%s|%s|%s|%s|%d", v.Monitor, e.ArgsHash, e.PathHash, e.Hash, e.Threshold)
}
