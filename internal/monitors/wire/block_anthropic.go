package wire

import (
	"encoding/json"
	"strings"

	"github.com/ethosagent/tollgate"
)

func stopFor(tcs []tollgate.ToolCall) string {
	if len(tcs) > 0 {
		return "tool_use"
	}
	return "end_turn"
}

func anthropicJSON(text string, tcs []tollgate.ToolCall) ([]byte, error) {
	content := []obj{{"type": "text", "text": text}}
	for _, tc := range tcs {
		content = append(content, obj{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": json.RawMessage(argsOrEmpty(tc.Arguments))})
	}
	return marshal(obj{
		"id": "msg_meterwise_block", "type": "message", "role": "assistant", "model": blockModel,
		"content": content, "stop_reason": stopFor(tcs), "stop_sequence": nil,
		"usage": obj{"input_tokens": 0, "output_tokens": 0},
	})
}

type frame struct {
	event string
	v     obj
}

func blockStart(idx int, block obj) frame {
	return frame{"content_block_start", obj{"type": "content_block_start", "index": idx, "content_block": block}}
}

func blockDelta(idx int, delta obj) frame {
	return frame{"content_block_delta", obj{"type": "content_block_delta", "index": idx, "delta": delta}}
}

func blockStop(idx int) frame {
	return frame{"content_block_stop", obj{"type": "content_block_stop", "index": idx}}
}

func anthropicStream(text string, tcs []tollgate.ToolCall) ([]byte, error) {
	frames := []frame{
		{"message_start", obj{"type": "message_start", "message": obj{
			"id": "msg_meterwise_block", "type": "message", "role": "assistant", "model": blockModel, "content": []obj{},
			"stop_reason": nil, "stop_sequence": nil, "usage": obj{"input_tokens": 0, "output_tokens": 0}}}},
		blockStart(0, obj{"type": "text", "text": ""}),
		blockDelta(0, obj{"type": "text_delta", "text": text}),
		blockStop(0),
	}
	for i, tc := range tcs {
		idx := i + 1
		frames = append(frames,
			blockStart(idx, obj{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": obj{}}),
			blockDelta(idx, obj{"type": "input_json_delta", "partial_json": argsOrEmpty(tc.Arguments)}),
			blockStop(idx))
	}
	frames = append(frames,
		frame{"message_delta", obj{"type": "message_delta", "delta": obj{"stop_reason": stopFor(tcs), "stop_sequence": nil}, "usage": obj{"output_tokens": 0}}},
		frame{"message_stop", obj{"type": "message_stop"}})
	var b strings.Builder
	for _, f := range frames {
		if err := sse(&b, f.event, f.v); err != nil {
			return nil, err
		}
	}
	return []byte(b.String()), nil
}
