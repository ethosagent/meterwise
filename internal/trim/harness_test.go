package trim_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/trim"
)

// step is one assistant tool call and the result it got.
type step struct {
	tool, args, result string
	err                bool // anthropic is_error
}

const (
	sysText  = "You are a careful coding agent. <b>Never</b> guess."
	toolDesc = "Runs a shell command in the workspace and returns its combined output. Use it for builds, tests and git. "
)

var wires = []tollgate.WireFormat{tollgate.WireOpenAIChat, tollgate.WireAnthropicMessages}

func tid(i int) string { return "call_" + string(rune('a'+i)) }

// requestBody renders the first n steps as the provider's request. "messages" is the last member, so a request's bytes
// up to the end of its messages are a prefix of the next request's.
func requestBody(t testing.TB, w tollgate.WireFormat, steps []step, cacheAt int, longDesc bool) []byte {
	t.Helper()
	desc := "Run a command"
	if longDesc {
		desc = toolDesc + toolDesc + toolDesc + toolDesc
	}
	var body any
	if w == tollgate.WireOpenAIChat {
		msgs := []any{map[string]any{"role": "system", "content": sysText}, map[string]any{"role": "user", "content": "fix the build"}}
		for i, s := range steps {
			msgs = append(msgs,
				map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": tid(i), "type": "function", "function": map[string]any{"name": s.tool, "arguments": s.args}}}},
				map[string]any{"role": "tool", "tool_call_id": tid(i), "content": s.result})
		}
		body = orderedBody{"model": "gpt-4.1", "stream": false, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "bash", "description": desc, "parameters": map[string]any{"type": "object"}}}}, "messages": msgs}
	} else {
		msgs := []any{map[string]any{"role": "user", "content": "fix the build"}}
		for i, s := range steps {
			blk := map[string]any{"type": "tool_result", "tool_use_id": tid(i), "content": s.result}
			if s.err {
				blk["is_error"] = true
			}
			if i == cacheAt {
				blk["cache_control"] = map[string]any{"type": "ephemeral"}
			}
			msgs = append(msgs,
				map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": tid(i), "name": s.tool, "input": json.RawMessage(s.args)}}},
				map[string]any{"role": "user", "content": []any{blk}})
		}
		body = orderedBody{"model": "claude-sonnet-4-5", "max_tokens": 64, "system": []any{map[string]any{"type": "text", "text": sysText, "cache_control": map[string]any{"type": "ephemeral"}}},
			"tools": []any{map[string]any{"name": "bash", "description": desc, "input_schema": map[string]any{"type": "object"}}}, "messages": msgs}
	}
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// orderedBody marshals with "messages" last and the rest in key order.
type orderedBody map[string]any

func (o orderedBody) MarshalJSON() ([]byte, error) {
	rest := map[string]any{}
	for k, v := range o {
		if k != "messages" {
			rest[k] = v
		}
	}
	head, err := json.Marshal(rest)
	if err != nil {
		return nil, err
	}
	msgs, err := json.Marshal(o["messages"])
	if err != nil {
		return nil, err
	}
	return append(append(head[:len(head)-1:len(head)-1], []byte(`,"messages":`)...), append(msgs, '}')...), nil
}

// neutral is the request's conversation as tollgate parses it: what the hook reads for tool-call arguments.
func neutral(steps []step) []tollgate.Message {
	msgs := []tollgate.Message{{Role: tollgate.RoleUser, Text: "fix the build"}}
	for i, s := range steps {
		msgs = append(msgs,
			tollgate.Message{Role: tollgate.RoleAssistant, ToolCalls: []tollgate.ToolCall{{ID: tid(i), Name: s.tool, Arguments: json.RawMessage(s.args)}}},
			tollgate.Message{Role: tollgate.RoleTool, ToolCallID: tid(i), Text: s.result})
	}
	return msgs
}

// session drives one conversation through a hook, one request per call.
type session struct {
	t     testing.TB
	h     *trim.Hook
	w     tollgate.WireFormat
	cfg   contract.TrimConfig
	id    string
	calls int
	cache int
	long  bool
}

// calm is the default options with an analysis budget no loaded test machine can exhaust, so a test that is not about
// the deadline never sees a region skipped for want of time.
func calm() trim.Options { return trim.Options{MaxWork: time.Hour} }

func newSession(t testing.TB, h *trim.Hook, w tollgate.WireFormat, mode contract.TrimMode) *session {
	cfg := contract.DefaultTrimConfig()
	cfg.Mode = mode
	return &session{t: t, h: h, w: w, cfg: cfg, id: "s1", cache: -1}
}

// send runs Before on a request carrying steps and returns the call afterwards.
func (s *session) send(steps []step) *tollgate.Call { return s.sendCtx(context.Background(), steps) }

func (s *session) sendCtx(ctx context.Context, steps []step) *tollgate.Call {
	s.t.Helper()
	c := s.build(steps)
	s.run(ctx, c)
	return c
}

// build makes the call for a request carrying steps, without running the hook.
func (s *session) build(steps []step) *tollgate.Call {
	s.calls++
	return &tollgate.Call{
		ID: "c", Tenant: tollgate.Tenant{ID: "ws", Attrs: map[string]string{trim.AttrTrim: s.cfg.Attr()}},
		Session:  tollgate.Session{ID: s.id, TenantID: "ws", Calls: s.calls},
		Provider: tollgate.Provider{Name: "p", Wire: s.w}, Model: "m",
		Body: requestBody(s.t, s.w, steps, s.cache, s.long), Messages: neutral(steps), Meta: map[string]any{},
	}
}

func (s *session) run(ctx context.Context, c *tollgate.Call) {
	s.t.Helper()
	resp, err := s.h.Before(ctx, c)
	if resp != nil || err != nil {
		s.t.Fatalf("trim must never short-circuit: %v %v", resp, err)
	}
}

// result returns the call's TrimResult.
func result(c *tollgate.Call) (contract.TrimResult, bool) {
	r, ok := c.Meta[contract.MetaTrim].(contract.TrimResult)
	return r, ok
}

// agentSteps is a coding agent's session: a long build log, a file read twice, a diff, JSON, test output and a grep.
func agentSteps() []step {
	return []step{
		{tool: "bash", args: `{"cmd":"make all"}`, result: buildLog()},
		{tool: "bash", args: `{"cmd":"cat package.json"}`, result: packageJSON() + packageJSON()},
		{tool: "bash", args: `{"cmd":"git diff -U12"}`, result: gitDiff()},
		{tool: "bash", args: `{"cmd":"cat package.json again"}`, result: packageJSON() + packageJSON()},
		{tool: "bash", args: `{"cmd":"curl /users"}`, result: jsonArray() + "\n"},
		{tool: "bash", args: `{"cmd":"pytest"}`, result: pytestRun()},
		{tool: "bash", args: `{"cmd":"grep -rn lookup src"}`, result: grepOut()},
	}
}
