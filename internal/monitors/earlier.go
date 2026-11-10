package monitors

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// resultLoc is where the earlier identical result of a blocked call is: the id of the tool call that
// received it.
type resultLoc struct{ earlierID string }

// earlierResult finds, in the request's own messages, an earlier tool result identical to the pending
// one: the pending call is the newest assistant turn's call that match accepts, its result is the tool
// message answering it, and the earlier call is an older call that match accepts whose result has the
// same text. Nothing is remembered between calls (ruling R4). It reports false when there is no such
// result, so the block degrades to a note.
func earlierResult(msgs []tollgate.Message, match func(tollgate.ToolCall) bool) (resultLoc, bool) {
	last := lastAssistant(msgs)
	if last < 0 {
		return resultLoc{}, false
	}
	pending := callIDOf(msgs[last], match)
	want, ok := resultOf(msgs, last, pending)
	if !ok {
		return resultLoc{}, false
	}
	for i := last - 1; i >= 0; i-- {
		if msgs[i].Role != tollgate.RoleAssistant {
			continue
		}
		id := callIDOf(msgs[i], match)
		if got, ok := resultOf(msgs, i, id); ok && got == want {
			return resultLoc{earlierID: id}, true
		}
	}
	return resultLoc{}, false
}

// lastAssistant is the newest assistant turn of msgs, -1 when none. A trailing assistant message is a
// prefill (the client starts the answer it asks for), not a turn, and is skipped.
func lastAssistant(msgs []tollgate.Message) int {
	end := len(msgs)
	if end > 0 && msgs[end-1].Role == tollgate.RoleAssistant {
		end--
	}
	for i := end - 1; i >= 0; i-- {
		if msgs[i].Role == tollgate.RoleAssistant {
			return i
		}
	}
	return -1
}

// callIDOf is the id of the first tool call of m that match accepts, "" when none or it has no id.
func callIDOf(m tollgate.Message, match func(tollgate.ToolCall) bool) string {
	for _, tc := range m.ToolCalls {
		if match(tc) {
			return tc.ID
		}
	}
	return ""
}

// resultOf returns the text of the tool message that answers call id, looking after message i until
// the next assistant turn.
func resultOf(msgs []tollgate.Message, i int, id string) (string, bool) {
	if id == "" {
		return "", false
	}
	for j := i + 1; j < len(msgs) && msgs[j].Role != tollgate.RoleAssistant; j++ {
		if msgs[j].Role == tollgate.RoleTool && msgs[j].ToolCallID == id {
			return msgs[j].Text, true
		}
	}
	return "", false
}

// matcher builds the tool-call predicate that finds the call a verdict is about: equal canonical
// arguments for duplicate_tool_call, the same path for repeated_read. Evidence holds only hashes, so
// the match is by the same workspace-salted hash.
func matcher(ws string, v contract.MonitorVerdict) func(tollgate.ToolCall) bool {
	id := func(h uint64) string { return contract.HashOf(ws, h) }
	switch v.Monitor {
	case contract.MonitorDuplicateToolCall:
		return func(tc tollgate.ToolCall) bool {
			return id(contract.ToolArgsHash(tc.Name, tc.Arguments)) == v.Evidence.ArgsHash
		}
	case contract.MonitorRepeatedRead:
		return func(tc tollgate.ToolCall) bool {
			return contract.ClassifyTool(tc.Name) == contract.ToolRead && id(contract.ToolSubject(tc.Name, tc.Arguments)) == v.Evidence.PathHash
		}
	}
	return func(tollgate.ToolCall) bool { return false }
}
