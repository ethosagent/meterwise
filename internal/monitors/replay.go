package monitors

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// replayTurn finds, in the request's own messages, the assistant turn that followed the earlier
// identical tool result: the pending call is the newest assistant turn's call that match accepts, its
// result is the tool message answering it, and the earlier call is an older call that match accepts
// whose result has the same text. The turn returned is the next assistant message after that earlier
// result. Nothing is remembered between calls (ruling R4). It reports false when there is no such
// turn, or when it would be the pending turn itself (replaying that would only repeat the call).
func replayTurn(msgs []tollgate.Message, match func(tollgate.ToolCall) bool) (tollgate.Message, bool) {
	last := lastAssistant(msgs)
	if last < 0 {
		return tollgate.Message{}, false
	}
	var pending *tollgate.ToolCall
	for i := range msgs[last].ToolCalls {
		if match(msgs[last].ToolCalls[i]) {
			pending = &msgs[last].ToolCalls[i]
			break
		}
	}
	if pending == nil {
		return tollgate.Message{}, false
	}
	want, ok := resultOf(msgs, last, pending.ID)
	if !ok {
		return tollgate.Message{}, false
	}
	for i := last - 1; i >= 0; i-- {
		if msgs[i].Role != tollgate.RoleAssistant || !hasMatch(msgs[i], match) {
			continue
		}
		if got, ok := resultOf(msgs, i, callIDOf(msgs[i], match)); ok && got == want {
			if j := nextAssistant(msgs, i); j >= 0 && j < last {
				return msgs[j], true
			}
		}
	}
	return tollgate.Message{}, false
}

func lastAssistant(msgs []tollgate.Message) int {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == tollgate.RoleAssistant {
			return i
		}
	}
	return -1
}

func hasMatch(m tollgate.Message, match func(tollgate.ToolCall) bool) bool {
	return callIDOf(m, match) != ""
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

func nextAssistant(msgs []tollgate.Message, i int) int {
	for j := i + 1; j < len(msgs); j++ {
		if msgs[j].Role == tollgate.RoleAssistant {
			return j
		}
	}
	return -1
}

// matcher builds the tool-call predicate that finds the call a verdict is about: equal canonical
// arguments for duplicate_tool_call, the same path for repeated_read. Evidence holds only hashes, so
// the match is by the same workspace-salted hash.
func matcher(ws string, v contract.MonitorVerdict) func(tollgate.ToolCall) bool {
	id := func(h uint64) string { return contract.HashID(ws, contract.HashValue(h)) }
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
