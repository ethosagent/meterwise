package wire

import "strings"

func openAIJSON(text string) ([]byte, error) {
	return marshal(obj{
		"id": "chatcmpl-meterwise-block", "object": "chat.completion", "created": 0, "model": blockModel,
		"choices": []obj{{"index": 0, "message": obj{"role": "assistant", "content": text}, "finish_reason": "stop"}},
		"usage":   obj{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	})
}

func openAIStream(text string) ([]byte, error) {
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
	if err := sse(&b, "", chunk(obj{}, "stop")); err != nil {
		return nil, err
	}
	b.WriteString("data: [DONE]\n\n")
	return []byte(b.String()), nil
}
