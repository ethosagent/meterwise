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

// blockNote is the note appended to a replayed turn when a call is blocked.
func blockNote(v contract.MonitorVerdict) string {
	return fmt.Sprintf("%sBlocked by the %s monitor (seen %d times, calls %v): this request repeats an earlier state, so the earlier answer is replayed and the model was not called.",
		notePrefix, v.Monitor, v.Evidence.Count, v.Evidence.MatchedCalls)
}

// dedupKey identifies what a note is about, per session: the monitor plus its subject.
func dedupKey(v contract.MonitorVerdict) string {
	e := v.Evidence
	return fmt.Sprintf("%s|%s|%s|%s|%d", v.Monitor, e.ArgsHash, e.PathHash, e.Hash, e.Threshold)
}
