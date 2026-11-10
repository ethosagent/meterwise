package wire

import "strings"

// anthropicMsg is the message object of both the JSON body and the stream's message_start: the same
// message, with its text and stop reason or still empty.
func anthropicMsg(content []obj, stop any) obj {
	return obj{
		"id": "msg_meterwise_block", "type": "message", "role": "assistant", "model": blockModel,
		"content": content, "stop_reason": stop, "stop_sequence": nil, "usage": obj{"input_tokens": 0, "output_tokens": 0},
	}
}

func anthropicJSON(text string) ([]byte, error) {
	return marshal(anthropicMsg([]obj{{"type": "text", "text": text}}, "end_turn"))
}

type frame struct {
	event string
	v     obj
}

func anthropicStream(text string) ([]byte, error) {
	frames := []frame{
		{"message_start", obj{"type": "message_start", "message": anthropicMsg([]obj{}, nil)}},
		{"content_block_start", obj{"type": "content_block_start", "index": 0, "content_block": obj{"type": "text", "text": ""}}},
		{"content_block_delta", obj{"type": "content_block_delta", "index": 0, "delta": obj{"type": "text_delta", "text": text}}},
		{"content_block_stop", obj{"type": "content_block_stop", "index": 0}},
		{"message_delta", obj{"type": "message_delta", "delta": obj{"stop_reason": "end_turn", "stop_sequence": nil}, "usage": obj{"output_tokens": 0}}},
		{"message_stop", obj{"type": "message_stop"}},
	}
	var b strings.Builder
	for _, f := range frames {
		if err := sse(&b, f.event, f.v); err != nil {
			return nil, err
		}
	}
	return []byte(b.String()), nil
}
