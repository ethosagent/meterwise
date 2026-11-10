package history_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
)

var bg = context.Background()

func newCall(tenant, session string, calls int, msgs ...tollgate.Message) *tollgate.Call {
	return &tollgate.Call{
		ID: fmt.Sprintf("c%d", calls), Tenant: tollgate.Tenant{ID: tenant}, Session: tollgate.Session{ID: session, Calls: calls},
		Provider: tollgate.Provider{Wire: tollgate.WireOpenAIChat}, Messages: msgs, Body: []byte(`{"messages":[]}`),
	}
}

func asst(text string, tools ...tollgate.ToolCall) tollgate.Message {
	return tollgate.Message{Role: tollgate.RoleAssistant, Text: text, ToolCalls: tools}
}

func tc(id, name, args string) tollgate.ToolCall {
	return tollgate.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(args)}
}

func ringOf(t testing.TB, c *tollgate.Call) []contract.CallSummary {
	t.Helper()
	v, ok := c.Meta[contract.MetaRing].(contract.RingView)
	if !ok {
		t.Fatalf("Meta[%q] = %T, want a RingView", contract.MetaRing, c.Meta[contract.MetaRing])
	}
	return v.Calls()
}

// run plays one streamed call through the hook: Before, the events, After. It returns the call.
func run(t testing.TB, h *history.Hook, c *tollgate.Call, in, out int64, events ...tollgate.Event) *tollgate.Call {
	t.Helper()
	if _, err := h.Before(bg, c); err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		h.OnEvent(bg, c, e)
	}
	h.After(bg, c, tollgate.Metric{Finish: tollgate.FinishOK, Status: 200, Usage: tollgate.Usage{InputTokens: in, OutputTokens: out}})
	return c
}

func text(s string) tollgate.Event { return tollgate.Event{Kind: tollgate.EventTextDelta, Text: s} }

func toolDelta(idx int, name, args string) tollgate.Event {
	return tollgate.Event{Kind: tollgate.EventToolCallDelta, ToolCall: &tollgate.ToolCallDelta{Index: idx, Name: name, Arguments: args}}
}
