package monitors_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
	"github.com/ethosagent/meterwise/internal/monitors/wire"
)

type obj = map[string]any

// wires are the two provider wires the monitors act on.
var wires = []tollgate.WireFormat{tollgate.WireOpenAIChat, tollgate.WireAnthropicMessages}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func user(text string) tollgate.Message { return tollgate.Message{Role: tollgate.RoleUser, Text: text} }

func asst(text string, tcs ...tollgate.ToolCall) tollgate.Message {
	return tollgate.Message{Role: tollgate.RoleAssistant, Text: text, ToolCalls: tcs}
}

func tcall(id, name, args string) tollgate.ToolCall {
	return tollgate.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func toolResult(id, text string) tollgate.Message {
	return tollgate.Message{Role: tollgate.RoleTool, ToolCallID: id, Text: text}
}

// requestBody renders neutral messages as the provider's request JSON.
func requestBody(t testing.TB, w tollgate.WireFormat, stream bool, msgs []tollgate.Message) []byte {
	t.Helper()
	var out []obj
	for _, m := range msgs {
		if w == tollgate.WireAnthropicMessages {
			out = append(out, anthropicMsg(m))
		} else {
			out = append(out, openaiMsg(m))
		}
	}
	body := obj{"model": "test-model", "stream": stream, "messages": out}
	if w == tollgate.WireAnthropicMessages {
		body["max_tokens"] = 1024
		body["system"] = "You are a test agent."
	}
	return mustJSON(t, body)
}

func openaiMsg(m tollgate.Message) obj {
	switch m.Role {
	case tollgate.RoleTool:
		return obj{"role": "tool", "tool_call_id": m.ToolCallID, "content": m.Text}
	case tollgate.RoleAssistant:
		o := obj{"role": "assistant", "content": m.Text}
		var calls []obj
		for _, tc := range m.ToolCalls {
			calls = append(calls, obj{"id": tc.ID, "type": "function", "function": obj{"name": tc.Name, "arguments": string(tc.Arguments)}})
		}
		if calls != nil {
			o["tool_calls"] = calls
		}
		return o
	}
	return obj{"role": string(m.Role), "content": m.Text}
}

func anthropicMsg(m tollgate.Message) obj {
	switch m.Role {
	case tollgate.RoleTool:
		return obj{"role": "user", "content": []obj{{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Text}}}
	case tollgate.RoleAssistant:
		var blocks []obj
		if m.Text != "" {
			blocks = append(blocks, obj{"type": "text", "text": m.Text})
		}
		for _, tc := range m.ToolCalls {
			blocks = append(blocks, obj{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": tc.Arguments})
		}
		return obj{"role": "assistant", "content": blocks}
	}
	return obj{"role": "user", "content": m.Text}
}

// weatherTranscript is an agent that asks for the same weather n times: n assistant turns, each with
// the same tool call and the same result, ending with the result pending (the next call's request).
func weatherTranscript(n int) []tollgate.Message {
	msgs := []tollgate.Message{user("what is the weather in Paris")}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("call_%d", i)
		msgs = append(msgs, asst("", tcall(id, "get_weather", `{"city":"Paris"}`)), toolResult(id, "sunny, 21C"))
	}
	return msgs
}

var parisHash = contract.ToolArgsHash("get_weather", []byte(`{"city":"Paris"}`))

type fakeRing []contract.CallSummary

func (f fakeRing) Calls() []contract.CallSummary { return f }

// dupRing is a ring whose newest call repeats an earlier get_weather call (calls 1 and 3).
func dupRing() fakeRing {
	return fakeRing{
		{Index: 1, Tools: []string{"get_weather"}, ArgsHashes: []uint64{parisHash}, Input: 100, Output: 10, BodyBytes: 40},
		{Index: 2, Tools: []string{"grep"}, ArgsHashes: []uint64{12}, Input: 150, Output: 30, BodyBytes: 80},
		{Index: 3, Tools: []string{"get_weather"}, ArgsHashes: []uint64{parisHash}, Input: 200, Output: 20, BodyBytes: 100},
	}
}

func attr(mod func(*contract.MonitorConfig)) map[string]string {
	cfg := contract.DefaultMonitorConfig()
	mod(&cfg)
	return map[string]string{"monitors": cfg.Attr()}
}

func dupMode(m contract.MonitorMode) map[string]string {
	return attr(func(c *contract.MonitorConfig) { c.DuplicateToolCall.Mode = m })
}

// before builds a call the way history leaves it for the monitors: ring and verdict pointer in Meta.
func before(t testing.TB, w tollgate.WireFormat, ring contract.RingView, attrs map[string]string, calls int, msgs []tollgate.Message) *tollgate.Call {
	t.Helper()
	return &tollgate.Call{
		ID: "c", Tenant: tollgate.Tenant{ID: "ws", Attrs: attrs}, Session: tollgate.Session{ID: "s1", TenantID: "ws", Calls: calls},
		Provider: tollgate.Provider{Wire: w}, Messages: msgs, Body: requestBody(t, w, false, msgs),
		Meta: map[string]any{contract.MetaRing: ring, contract.MetaMonitors: &contract.CallVerdicts{}},
	}
}

func verdictsOf(c *tollgate.Call) ([]contract.MonitorVerdict, int) {
	return c.Meta[contract.MetaMonitors].(*contract.CallVerdicts).Seal()
}

func countProxy(b []byte) int { return bytes.Count(b, []byte("[proxy]")) }

// newMon builds the Hook with the wire editor the host injects (archtest H3 keeps monitors from importing wire).
func newMon(o monitors.Options) *monitors.Hook {
	if o.Editor == nil {
		o.Editor = wire.Editor{}
	}
	return monitors.New(o)
}

var _ monitors.Editor = wire.Editor{}
