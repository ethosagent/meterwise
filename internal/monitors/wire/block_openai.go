package wire

import (
	"strings"

	"github.com/ethosagent/tollgate"
)

func oaToolCalls(tcs []tollgate.ToolCall, withIndex bool) []obj {
	out := make([]obj, 0, len(tcs))
	for i, tc := range tcs {
		o := obj{"id": tc.ID, "type": "function", "function": obj{"name": tc.Name, "arguments": argsOrEmpty(tc.Arguments)}}
		if withIndex {
			o["index"] = i
		}
		out = append(out, o)
	}
	return out
}

func finishFor(tcs []tollgate.ToolCall) string {
	if len(tcs) > 0 {
		return "tool_calls"
	}
	return "stop"
}

func openAIJSON(text string, tcs []tollgate.ToolCall) ([]byte, error) {
	msg := obj{"role": "assistant", "content": text}
	if len(tcs) > 0 {
		msg["tool_calls"] = oaToolCalls(tcs, false)
	}
	return marshal(obj{
		"id": "chatcmpl-meterwise-block", "object": "chat.completion", "created": 0, "model": blockModel,
		"choices": []obj{{"index": 0, "message": msg, "finish_reason": finishFor(tcs)}},
		"usage":   obj{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	})
}

func openAIStream(text string, tcs []tollgate.ToolCall) ([]byte, error) {
	var b strings.Builder
	chunk := func(delta obj, finish any) obj {
		return obj{
			"id": "chatcmpl-meterwise-block", "object": "chat.completion.chunk", "created": 0, "model": blockModel,
			"choices": []obj{{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}
	if err := sse(&b, "", chunk(obj{"role": "assistant", "content": text}, nil)); err != nil {
		return nil, err
	}
	if len(tcs) > 0 {
		if err := sse(&b, "", chunk(obj{"tool_calls": oaToolCalls(tcs, true)}, nil)); err != nil {
			return nil, err
		}
	}
	if err := sse(&b, "", chunk(obj{}, finishFor(tcs))); err != nil {
		return nil, err
	}
	b.WriteString("data: [DONE]\n\n")
	return []byte(b.String()), nil
}
