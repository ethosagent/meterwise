package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
)

// scenarioHeader picks the canned reply of the loopback fake upstream; weightHeader scales its usage so
// the numbers vary from call to call. Neither header leaves the process: the upstream is ours.
const (
	scenarioHeader = "x-demo-scenario"
	weightHeader   = "x-demo-weight"
)

// scenario is one synthetic exchange: the request the "client" sends and the reply the fake upstream
// gives. The bodies are made up for the demo; no real prompt or key appears anywhere.
type scenario struct {
	name    string
	path    string
	request string
	reply   func(in, out int) (status int, contentType, body string)
}

const openAIRequest = `{"model":"gpt-4.1-mini","stream":%t,"stream_options":{"include_usage":true},"messages":[{"role":"system","content":"You are a demo agent."},{"role":"user","content":"Say hello."}]}`

const anthropicRequest = `{"model":"claude-sonnet-4-5","max_tokens":256,"stream":%t,"system":"You are a demo agent.","messages":[{"role":"user","content":"Say hello."}]}`

func openAIChunk(delta, finish, usage string) string {
	choices := `[{"index":0,"delta":` + delta + `,"finish_reason":` + finish + `}]`
	if usage != "" {
		choices = "[]"
	}
	u := ""
	if usage != "" {
		u = `,"usage":` + usage
	}
	return `data: {"id":"chatcmpl-demo","object":"chat.completion.chunk","created":1762500000,"model":"gpt-4.1-mini","choices":` + choices + u + "}\n\n"
}

func openAIUsage(in, out int) string {
	return fmt.Sprintf(`{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d}`, in, out, in+out)
}

var plainScenarios = []scenario{
	{"openai-tools", "/openai/v1/chat/completions", fmt.Sprintf(openAIRequest, true), func(in, out int) (int, string, string) {
		return 200, "text/event-stream", openAIChunk(`{"role":"assistant","content":null,"tool_calls":[{"index":0,"id":"call_demo","type":"function","function":{"name":"read_file","arguments":""}}]}`, "null", "") +
			openAIChunk(`{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":\"main.go\"}"}}]}`, "null", "") +
			openAIChunk(`{}`, `"tool_calls"`, "") + openAIChunk("", "", openAIUsage(in, out)) + "data: [DONE]\n\n"
	}},
	{"openai-text", "/openai/v1/chat/completions", fmt.Sprintf(openAIRequest, true), func(in, out int) (int, string, string) {
		return 200, "text/event-stream", openAIChunk(`{"role":"assistant","content":""}`, "null", "") + openAIChunk(`{"content":"Hello world"}`, "null", "") +
			openAIChunk(`{}`, `"stop"`, "") + openAIChunk("", "", openAIUsage(in, out)) + "data: [DONE]\n\n"
	}},
	{"openai-json", "/openai/v1/chat/completions", fmt.Sprintf(openAIRequest, false), func(in, out int) (int, string, string) {
		return 200, "application/json", `{"id":"chatcmpl-demo","object":"chat.completion","created":1762500000,"model":"gpt-4.1-mini","choices":[{"index":0,"message":{"role":"assistant","content":"Hello world"},"finish_reason":"stop"}],"usage":` + openAIUsage(in, out) + `}`
	}},
	{"anthropic-text", "/anthropic/v1/messages", fmt.Sprintf(anthropicRequest, true), func(in, out int) (int, string, string) {
		return 200, "text/event-stream", fmt.Sprintf("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_demo\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-5\",\"content\":[],\"usage\":{\"input_tokens\":%d,\"output_tokens\":1}}}\n\n", in) +
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello world\"}}\n\n" +
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
			fmt.Sprintf("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":%d}}\n\n", out) +
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	}},
	{"anthropic-json", "/anthropic/v1/messages", fmt.Sprintf(anthropicRequest, false), func(in, out int) (int, string, string) {
		return 200, "application/json", fmt.Sprintf(`{"id":"msg_demo","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"Hello world"}],"stop_reason":"end_turn","usage":{"input_tokens":%d,"output_tokens":%d}}`, in, out)
	}},
	{"openai-500", "/openai/v1/chat/completions", fmt.Sprintf(openAIRequest, false), func(int, int) (int, string, string) {
		return 500, "application/json", `{"error":{"message":"The server had an error while processing your request.","type":"server_error","param":null,"code":null}}`
	}},
}

// scenarios is every canned exchange: the plain ones above and one tool-call reply per looping-agent turn.
var scenarios = append(slices.Clone(plainScenarios), loopScenarios()...)

// scenarioByName finds a scenario; the script below is made of names.
func scenarioByName(name string) (scenario, bool) {
	for _, s := range scenarios {
		if s.name == name {
			return s, true
		}
	}
	return scenario{}, false
}

// upstream is the loopback fake upstream. Hits counts the requests that reached it: a blocked call never
// does, so the count against what the clients sent shows what the monitors kept off the provider.
type upstream struct {
	*httptest.Server
	hits atomic.Int64
}

// newUpstream starts the loopback fake upstream.
func newUpstream() *upstream {
	u := &upstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		s, ok := scenarioByName(r.Header.Get(scenarioHeader))
		if !ok {
			http.Error(w, "unknown demo scenario", http.StatusNotFound)
			return
		}
		n, _ := strconv.Atoi(r.Header.Get(weightHeader))
		status, ct, body := s.reply(40+n*7, 5+n*3)
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
		if strings.HasPrefix(ct, "text/event-stream") {
			for _, frame := range strings.SplitAfter(body, "\n\n") {
				_, _ = w.Write([]byte(frame))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	return u
}
