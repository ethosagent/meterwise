package main

import (
	"encoding/json"
	"fmt"
)

// loopScenarioName is the scenarioHeader value of one tool-call turn of a looping agent. The fake upstream
// has one scenario per kind, wire, stream mode and turn, so its reply is a lookup, never built from a request.
func loopScenarioName(kind string, w wireShape, stream bool, turn int) string {
	return fmt.Sprintf("loop-%s-%s-%s-%d", kind, w.name, pick(stream, "stream", "json"), turn)
}

func loopScenarios() []scenario {
	var out []scenario
	for _, kind := range loopKinds {
		for _, w := range wireShapes {
			for _, stream := range []bool{true, false} {
				for turn := 0; turn < loopTurns; turn++ {
					args := kind.args(turn)
					out = append(out, scenario{
						name: loopScenarioName(kind.name, w, stream, turn),
						reply: func(in, o int) (int, string, string) {
							return toolReply(w.anthropic, stream, kind.tool, args, kind.text, in, o)
						},
					})
				}
			}
		}
	}
	return out
}

// toolReply is the fake upstream's tool-call answer in the Anthropic or OpenAI wire, streamed or not.
// The assistant text, when there is one, comes before the call: the next request will carry the same turn,
// and the monitors compare what the stream showed with what the client sends back.
func toolReply(anthropic, stream bool, tool, args, text string, in, out int) (status int, contentType, body string) {
	switch {
	case anthropic && stream:
		return 200, "text/event-stream", anthropicToolStream(tool, args, text, in, out)
	case anthropic:
		blocks := `{"type":"tool_use","id":"toolu_demo","name":` + quote(tool) + `,"input":` + args + `}`
		if text != "" {
			blocks = `{"type":"text","text":` + quote(text) + `},` + blocks
		}
		return 200, "application/json", fmt.Sprintf(`{"id":"msg_demo","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[%s],"stop_reason":"tool_use","usage":{"input_tokens":%d,"output_tokens":%d}}`, blocks, in, out)
	case stream:
		return 200, "text/event-stream", openAIToolStream(tool, args, text, in, out)
	}
	content := "null"
	if text != "" {
		content = quote(text)
	}
	call := fmt.Sprintf(`{"id":"call_demo","type":"function","function":{"name":%s,"arguments":%s}}`, quote(tool), quote(args))
	return 200, "application/json", `{"id":"chatcmpl-demo","object":"chat.completion","created":1762500000,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":` + content + `,"tool_calls":[` + call + `]},"finish_reason":"tool_calls"}],"usage":` + openAIUsage(in, out) + `}`
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func openAIToolStream(tool, args, text string, in, out int) string {
	head := `{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_demo","type":"function","function":{"name":` + quote(tool) + `,"arguments":""}}]}`
	part := `{"tool_calls":[{"index":0,"function":{"arguments":` + quote(args) + `}}]}`
	said := ""
	if text != "" {
		said = openAIChunk(`{"role":"assistant","content":`+quote(text)+`}`, "null", "")
	}
	return said + openAIChunk(head, "null", "") + openAIChunk(part, "null", "") + openAIChunk(`{}`, `"tool_calls"`, "") +
		openAIChunk("", "", openAIUsage(in, out)) + "data: [DONE]\n\n"
}

func anthropicToolStream(tool, args, text string, in, out int) string {
	idx, said := 0, ""
	if text != "" {
		idx = 1
		said = "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":" + quote(text) + "}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"
	}
	return fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_demo\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-5\",\"content\":[],\"usage\":{\"input_tokens\":%d,\"output_tokens\":1}}}\n\n", in) + said +
		fmt.Sprintf("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":%d,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_demo\",\"name\":%s,\"input\":{}}}\n\n", idx, quote(tool)) +
		fmt.Sprintf("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":%d,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}\n\n", idx, quote(args)) +
		fmt.Sprintf("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":%d}\n\n", idx) +
		fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":%d}}\n\n", out) +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
}
