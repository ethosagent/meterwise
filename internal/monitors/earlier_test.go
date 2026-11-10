package monitors

import (
	"encoding/json"
	"testing"

	"github.com/ethosagent/tollgate"
)

func call(id, name, args string) tollgate.ToolCall {
	return tollgate.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func asstMsg(text string, tcs ...tollgate.ToolCall) tollgate.Message {
	return tollgate.Message{Role: tollgate.RoleAssistant, Text: text, ToolCalls: tcs}
}

func result(id, text string) tollgate.Message {
	return tollgate.Message{Role: tollgate.RoleTool, ToolCallID: id, Text: text}
}

func TestEarlierResult(t *testing.T) {
	user := tollgate.Message{Role: tollgate.RoleUser, Text: "task"}
	paris := func(id string) tollgate.ToolCall { return call(id, "get_weather", `{"city":"Paris"}`) }
	isParis := func(tc tollgate.ToolCall) bool { return tc.Name == "get_weather" }
	cases := []struct {
		name string
		msgs []tollgate.Message
		want string // id of the earlier identical result, "" when none
	}{
		{"an earlier identical result two turns back",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "sunny"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"),
				asstMsg("three", paris("c")), result("c", "sunny")}, "a"},
		{"a repeat right after itself points at the turn before",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "sunny"), asstMsg("two", paris("b")), result("b", "sunny")}, "a"},
		{"the newest identical result is the one named",
			[]tollgate.Message{user, asstMsg("", paris("a")), result("a", "sunny"), asstMsg("", paris("b")), result("b", "sunny"),
				asstMsg("", paris("c")), result("c", "sunny")}, "b"},
		{"a trailing prefill is not the pending turn",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "sunny"), asstMsg("two", paris("b")), result("b", "sunny"), asstMsg("Answer:")}, "a"},
		{"the earlier result differs, so it is not the same state",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "rainy"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"),
				asstMsg("three", paris("c")), result("c", "sunny")}, ""},
		{"the pending call has no result yet",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "sunny"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"),
				asstMsg("three", paris("c")), call2(paris("d"))}, ""},
		{"no assistant turn at all", []tollgate.Message{user}, ""},
		{"the newest assistant turn made another call", []tollgate.Message{user, asstMsg("one", paris("a")), result("a", "s"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x")}, ""},
		{"an earlier identical call without an id cannot be matched to a result",
			[]tollgate.Message{user, asstMsg("one", paris("")), result("", "sunny"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"),
				asstMsg("three", paris("c")), result("c", "sunny")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := earlierResult(tc.msgs, isParis)
			if ok != (tc.want != "") || got.earlierID != tc.want {
				t.Errorf("earlierResult = %q, %v; want %q", got.earlierID, ok, tc.want)
			}
		})
	}
}

// call2 is a user message that answers nothing (the pending call's result is missing).
func call2(tollgate.ToolCall) tollgate.Message {
	return tollgate.Message{Role: tollgate.RoleUser, Text: "?"}
}
