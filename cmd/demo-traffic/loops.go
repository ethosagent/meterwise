package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// A looping agent is a client that keeps asking for the same thing. Each request carries the whole
// conversation so far (that is how the monitors see the loop: history is rebuilt from the request), so the
// client is real code, not a canned body: it appends the assistant's tool call and the tool's result to its
// messages and asks again, and it stops when the reply is not a tool call.

// loopTurns is how many tool calls a looping agent makes before the fake upstream finally answers in text.
const loopTurns = 4

// loopKind is one way to loop. The tool, its arguments and its result are what the monitors look at.
type loopKind struct {
	name   string // the monitor it is built to trigger
	prompt string
	text   string // what the assistant says alongside every tool call ("" for nothing)
	tool   string
	args   func(turn int) string // the tool call's JSON arguments
	result func(turn int) string
}

var loopKinds = []loopKind{
	{
		name: "duplicate_tool_call", prompt: "What is the weather in Paris?",
		tool: "get_weather", args: func(int) string { return `{"city":"Paris"}` }, result: func(int) string { return "sunny, 21C" },
	},
	{
		// Same file again and again; the offset differs, so the calls are not duplicates, only repeated reads.
		name: "repeated_read", prompt: "Why does main.go fail to build?",
		tool: "read_file", args: func(t int) string { return fmt.Sprintf(`{"path":"main.go","offset":%d}`, t*40) },
		result: func(int) string { return "package main\n\nfunc main() { run() }" },
	},
	{
		// A different search each turn and the same words about it: no write, nothing new read.
		name: "no_progress", prompt: "Find out why the build fails.",
		text: "I am still searching for the cause of the build failure, so I will try another query.",
		tool: "search_code", args: func(t int) string { return fmt.Sprintf(`{"query":"build failure attempt %d"}`, t) },
		result: func(int) string { return "no matches" },
	},
}

// wireShape is what differs between the two wires in a request and a reply.
type wireShape struct {
	name      string // the path prefix: openai or anthropic
	path      string
	model     string
	text      func(stream bool) string // the final text-answer scenario
	anthropic bool
}

var wireShapes = [2]wireShape{
	{name: "openai", path: "/openai/v1/chat/completions", model: "gpt-4.1-mini", text: func(s bool) string { return pick(s, "openai-text", "openai-json") }},
	{name: "anthropic", path: "/anthropic/v1/messages", model: "claude-sonnet-4-5", text: func(s bool) string { return pick(s, "anthropic-text", "anthropic-json") }, anthropic: true},
}

func pick(c bool, a, b string) string {
	if c {
		return a
	}
	return b
}

// variantOf spreads the four wire x stream combinations over rounds: kind k of round r takes (r+k)%4.
func variantOf(round, kind int) (w wireShape, stream bool) {
	v := (round + kind) % 4
	return wireShapes[v/2], v%2 == 0
}

// body renders the request of the turn-th call: the prompt plus turn finished tool calls.
func (k loopKind) body(w wireShape, stream bool, turn int) ([]byte, error) {
	msgs := []map[string]any{{"role": "user", "content": k.prompt}}
	for i := 0; i < turn; i++ {
		id := fmt.Sprintf("call_%d", i+1)
		args := json.RawMessage(k.args(i))
		if w.anthropic {
			blocks := []map[string]any{{"type": "tool_use", "id": id, "name": k.tool, "input": args}}
			if k.text != "" {
				blocks = append([]map[string]any{{"type": "text", "text": k.text}}, blocks...)
			}
			msgs = append(msgs, map[string]any{"role": "assistant", "content": blocks},
				map[string]any{"role": "user", "content": []map[string]any{{"type": "tool_result", "tool_use_id": id, "content": k.result(i)}}})
			continue
		}
		msgs = append(msgs, map[string]any{"role": "assistant", "content": k.text,
			"tool_calls": []map[string]any{{"id": id, "type": "function", "function": map[string]any{"name": k.tool, "arguments": string(args)}}}},
			map[string]any{"role": "tool", "tool_call_id": id, "content": k.result(i)})
	}
	doc := map[string]any{"model": w.model, "stream": stream, "messages": msgs}
	if w.anthropic {
		doc["max_tokens"] = 1024
		doc["system"] = "You are a demo agent."
	} else {
		doc["stream_options"] = map[string]any{"include_usage": true}
	}
	return json.Marshal(doc)
}

// asksForATool reports whether a reply (any wire, streamed or not) is a tool call. A block answer and a
// final answer are text turns: the client stops there.
func asksForATool(reply []byte) bool {
	s := string(reply)
	return strings.Contains(s, `"tool_calls"`) || strings.Contains(s, `"tool_use"`)
}
