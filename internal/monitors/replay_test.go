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

func TestReplayTurn(t *testing.T) {
	user := tollgate.Message{Role: tollgate.RoleUser, Text: "task"}
	paris := func(id string) tollgate.ToolCall { return call(id, "get_weather", `{"city":"Paris"}`) }
	isParis := func(tc tollgate.ToolCall) bool { return tc.Name == "get_weather" }
	cases := []struct {
		name string
		msgs []tollgate.Message
		want string // text of the replayed turn, "" when none
	}{
		{"the turn after the earlier identical result",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "sunny"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"),
				asstMsg("three", paris("c")), result("c", "sunny")}, "two"},
		{"a repeat right after itself has no distinct turn to replay",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "sunny"), asstMsg("two", paris("b")), result("b", "sunny")}, ""},
		{"the earlier result differs, so it is not the same state",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "rainy"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"),
				asstMsg("three", paris("c")), result("c", "sunny")}, ""},
		{"the pending call has no result yet",
			[]tollgate.Message{user, asstMsg("one", paris("a")), result("a", "sunny"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"), asstMsg("three", paris("c"))}, ""},
		{"no assistant turn at all", []tollgate.Message{user}, ""},
		{"the newest assistant turn made another call", []tollgate.Message{user, asstMsg("one", paris("a")), result("a", "s"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x")}, ""},
		{"an earlier identical call without an id cannot be matched to a result",
			[]tollgate.Message{user, asstMsg("one", paris("")), result("", "sunny"), asstMsg("two", call("b", "grep", `{}`)), result("b", "x"),
				asstMsg("three", paris("c")), result("c", "sunny")}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := replayTurn(tc.msgs, isParis)
			if ok != (tc.want != "") || got.Text != tc.want {
				t.Errorf("replayTurn = %q, %v; want %q", got.Text, ok, tc.want)
			}
		})
	}
}
