package monitors_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// The looping-agent e2e (B1): a simulated SDK client runs every tool a response names, appends the tool
// results and calls again, until a response names no tool. The upstream is a scripted model: each real
// call answers with the next scripted tool call, in the call's own wire and stream mode, and the final
// answer once the script is done. A block must end the loop (a terminal, text-only turn), never feed it.

// scripted is a fake model upstream that answers with the next scripted city, then a final text.
type scripted struct {
	mu     sync.Mutex
	script []string
	calls  int
	bodies [][]byte
}

func (s *scripted) push(cities ...string) {
	s.mu.Lock()
	s.script = append(s.script, cities...)
	s.mu.Unlock()
}

func (s *scripted) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func (s *scripted) last() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[len(s.bodies)-1]
}

func (s *scripted) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &req)
	s.mu.Lock()
	s.calls++
	s.bodies = append(s.bodies, body)
	turn := asst("The weather is sunny everywhere I looked.")
	if len(s.script) > 0 {
		city := s.script[0]
		s.script = s.script[1:]
		turn = asst("", tcall(fmt.Sprintf("toolu_up%02d", s.calls), "get_weather", `{"city":"`+city+`"}`))
	}
	s.mu.Unlock()
	anthropic := strings.HasSuffix(r.URL.Path, "/messages")
	ct := "application/json"
	if req.Stream {
		ct = "text/event-stream"
	}
	w.Header().Set("Content-Type", ct)
	_, _ = io.WriteString(w, renderTurn(anthropic, req.Stream, turn))
}

// renderTurn is a model response holding turn, as the provider sends it.
func renderTurn(anthropic, stream bool, turn tollgate.Message) string {
	j := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	tc := turn.ToolCalls
	switch {
	case !anthropic && !stream:
		msg := obj{"role": "assistant", "content": turn.Text}
		finish := "stop"
		if len(tc) > 0 {
			msg["tool_calls"] = []obj{{"id": tc[0].ID, "type": "function", "function": obj{"name": tc[0].Name, "arguments": string(tc[0].Arguments)}}}
			finish = "tool_calls"
		}
		return j(obj{"id": "chatcmpl-1", "object": "chat.completion", "model": "test-model",
			"choices": []obj{{"index": 0, "message": msg, "finish_reason": finish}}, "usage": obj{"prompt_tokens": 120, "completion_tokens": 12, "total_tokens": 132}})
	case !anthropic:
		chunk := func(delta obj, finish any) string {
			return "data: " + j(obj{"id": "chatcmpl-1", "object": "chat.completion.chunk", "model": "test-model",
				"choices": []obj{{"index": 0, "delta": delta, "finish_reason": finish}}}) + "\n\n"
		}
		out, finish := chunk(obj{"role": "assistant", "content": turn.Text}, nil), "stop"
		if len(tc) > 0 {
			out += chunk(obj{"tool_calls": []obj{{"index": 0, "id": tc[0].ID, "type": "function", "function": obj{"name": tc[0].Name, "arguments": string(tc[0].Arguments)}}}}, nil)
			finish = "tool_calls"
		}
		out += chunk(obj{}, finish)
		out += "data: " + j(obj{"id": "chatcmpl-1", "object": "chat.completion.chunk", "model": "test-model", "choices": []obj{},
			"usage": obj{"prompt_tokens": 120, "completion_tokens": 12, "total_tokens": 132}}) + "\n\n"
		return out + "data: [DONE]\n\n"
	case !stream:
		content, stop := []obj{{"type": "text", "text": turn.Text}}, "end_turn"
		if len(tc) > 0 {
			content, stop = []obj{{"type": "tool_use", "id": tc[0].ID, "name": tc[0].Name, "input": tc[0].Arguments}}, "tool_use"
		}
		return j(obj{"id": "msg_1", "type": "message", "role": "assistant", "model": "test-model", "content": content,
			"stop_reason": stop, "usage": obj{"input_tokens": 120, "output_tokens": 12}})
	}
	ev := func(name string, v obj) string { return "event: " + name + "\ndata: " + j(v) + "\n\n" }
	out := ev("message_start", obj{"type": "message_start", "message": obj{"id": "msg_1", "type": "message", "role": "assistant",
		"model": "test-model", "content": []obj{}, "usage": obj{"input_tokens": 120, "output_tokens": 0}}})
	stop := "end_turn"
	if len(tc) > 0 {
		stop = "tool_use"
		out += ev("content_block_start", obj{"type": "content_block_start", "index": 0, "content_block": obj{"type": "tool_use", "id": tc[0].ID, "name": tc[0].Name, "input": obj{}}})
		out += ev("content_block_delta", obj{"type": "content_block_delta", "index": 0, "delta": obj{"type": "input_json_delta", "partial_json": string(tc[0].Arguments)}})
	} else {
		out += ev("content_block_start", obj{"type": "content_block_start", "index": 0, "content_block": obj{"type": "text", "text": ""}})
		out += ev("content_block_delta", obj{"type": "content_block_delta", "index": 0, "delta": obj{"type": "text_delta", "text": turn.Text}})
	}
	out += ev("content_block_stop", obj{"type": "content_block_stop", "index": 0})
	out += ev("message_delta", obj{"type": "message_delta", "delta": obj{"stop_reason": stop}, "usage": obj{"output_tokens": 12}})
	return out + ev("message_stop", obj{"type": "message_stop"})
}

// sdkTurn reads the assistant turn and its stop reason from a response the way an SDK does.
func sdkTurn(t *testing.T, v variant, body []byte) (tollgate.Message, string) {
	t.Helper()
	m := tollgate.Message{Role: tollgate.RoleAssistant}
	var stop string
	add := func(id, name, args string) { m.ToolCalls = append(m.ToolCalls, tcall(id, name, args)) }
	frames := []string{string(body)}
	if v.stream {
		frames = nil
		for _, f := range strings.Split(strings.TrimSpace(string(body)), "\n\n") {
			if i := strings.Index(f, "data: "); i >= 0 && f[i+6:] != "[DONE]" {
				frames = append(frames, f[i+6:])
			}
		}
	}
	type part struct{ id, name, args string }
	var parts []*part
	for _, f := range frames {
		var doc struct {
			Choices []struct {
				Message, Delta struct {
					Content   string
					ToolCalls []struct {
						ID       string
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
				}
				FinishReason *string `json:"finish_reason"`
			}
			StopReason string `json:"stop_reason"`
			Content    []struct {
				Type, Text, ID, Name string
				Input                json.RawMessage
			}
			ContentBlock struct{ Type, ID, Name string } `json:"content_block"`
			Delta        struct {
				Text        string
				StopReason  string `json:"stop_reason"`
				PartialJSON string `json:"partial_json"`
			}
		}
		if err := json.Unmarshal([]byte(f), &doc); err != nil {
			t.Fatalf("an SDK cannot parse %q: %v", f, err)
		}
		for _, ch := range doc.Choices {
			m.Text += ch.Message.Content + ch.Delta.Content
			for _, x := range append(ch.Message.ToolCalls, ch.Delta.ToolCalls...) {
				if x.ID != "" {
					parts = append(parts, &part{id: x.ID, name: x.Function.Name})
				}
				if len(parts) > 0 {
					parts[len(parts)-1].args += x.Function.Arguments
				}
			}
			if ch.FinishReason != nil {
				stop = *ch.FinishReason
			}
		}
		for _, c := range doc.Content {
			switch c.Type {
			case "text":
				m.Text += c.Text
			case "tool_use":
				add(c.ID, c.Name, string(c.Input))
			}
		}
		if doc.StopReason != "" {
			stop = doc.StopReason
		}
		if doc.ContentBlock.Type == "tool_use" {
			parts = append(parts, &part{id: doc.ContentBlock.ID, name: doc.ContentBlock.Name})
		}
		m.Text += doc.Delta.Text
		if doc.Delta.PartialJSON != "" && len(parts) > 0 {
			parts[len(parts)-1].args += doc.Delta.PartialJSON
		}
		if doc.Delta.StopReason != "" {
			stop = doc.Delta.StopReason
		}
	}
	for _, p := range parts {
		add(p.id, p.name, p.args)
	}
	return m, stop
}

type agentRun struct {
	calls, blocked int
	finished       bool
	msgs           []tollgate.Message
	blockedTurns   []tollgate.Message
	stops          []string
}

// runAgent is the client loop: call, run every tool the response names, repeat; it stops on a turn
// with no tool call, or after max calls. A call is blocked when the upstream did not see it.
func runAgent(t *testing.T, r *rig, up *scripted, v variant, msgs []tollgate.Message, maxCalls int) agentRun {
	t.Helper()
	out := agentRun{msgs: msgs}
	ids := map[string]bool{}
	for out.calls < maxCalls {
		before := up.count()
		rep := r.call(v.wire, v.stream, out.msgs)
		out.calls++
		if rep.status != 200 {
			t.Fatalf("call %d: status %d: %s", out.calls, rep.status, rep.body)
		}
		turn, stop := sdkTurn(t, v, rep.body)
		if up.count() == before {
			out.blocked++
			out.blockedTurns = append(out.blockedTurns, turn)
			out.stops = append(out.stops, stop)
		}
		out.msgs = append(out.msgs, turn)
		if len(turn.ToolCalls) == 0 {
			out.finished = true
			return out
		}
		for _, tc := range turn.ToolCalls {
			if ids[tc.ID] {
				t.Errorf("call %d: tool call id %s was already used in this conversation", out.calls, tc.ID)
			}
			ids[tc.ID] = true
			var a map[string]string
			_ = json.Unmarshal(tc.Arguments, &a)
			out.msgs = append(out.msgs, toolResult(tc.ID, "weather in "+a["city"]+": sunny, 21C"))
		}
	}
	return out
}

func loopRig(t *testing.T, attrs map[string]string) (*rig, *scripted) {
	up := &scripted{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	return newRigAt(t, attrs, monitors.Options{}, srv.URL), up
}

// assertTerminal: a blocked answer is a text-only turn that ends the agent's turn and says why.
func assertTerminal(t *testing.T, v variant, res agentRun) {
	t.Helper()
	wantStop := "stop"
	if v.wire == tollgate.WireAnthropicMessages {
		wantStop = "end_turn"
	}
	for i, turn := range res.blockedTurns {
		if len(turn.ToolCalls) != 0 || res.stops[i] != wantStop || countProxy([]byte(turn.Text)) != 1 {
			t.Errorf("blocked answer %d is not a terminal text turn: stop %q, %d tool calls, text %q", i, res.stops[i], len(turn.ToolCalls), turn.Text)
		}
	}
}

func TestLoopingAgentFinishesUnderBlockOnBothWiresAndStreamModes(t *testing.T) {
	scripts := map[string][]string{"same call three times": {"Paris", "Paris", "Paris"}, "A-B-A": {"Paris", "London", "Paris"}}
	for _, v := range variants() {
		for name, script := range scripts {
			t.Run(v.String()+"/"+name, func(t *testing.T) {
				r, up := loopRig(t, dupMode(contract.ModeBlock))
				up.push(script...)
				res := runAgent(t, r, up, v, []tollgate.Message{user("what is the weather")}, 12)
				if !res.finished || res.blocked != 1 {
					t.Fatalf("finished %v after %d calls, %d blocked: the agent must end on the block", res.finished, res.calls, res.blocked)
				}
				if up.count() > len(script) {
					t.Errorf("upstream calls = %d, want at most %d (the script before the repeat)", up.count(), len(script))
				}
				assertTerminal(t, v, res)
			})
		}
	}
}

// After a block the user asks again and the model repeats the same call: the block was spent on that
// subject in this session, so the repeat is noted and goes to the model, and the agent still finishes.
func TestSecondRepeatOfABlockedSubjectIsNotedNotBlocked(t *testing.T) {
	for _, v := range variants() {
		t.Run(v.String(), func(t *testing.T) {
			r, up := loopRig(t, dupMode(contract.ModeBlock))
			up.push("Paris", "Paris")
			res := runAgent(t, r, up, v, []tollgate.Message{user("what is the weather")}, 12)
			if !res.finished || res.blocked != 1 {
				t.Fatalf("first run: finished %v, blocked %d", res.finished, res.blocked)
			}
			up.push("Paris")
			before, notes := up.count(), r.mon.Status()["notes_injected"].(int64)
			res2 := runAgent(t, r, up, v, append(res.msgs, user("please check Paris again")), 12)
			if !res2.finished || res2.blocked != 0 {
				t.Fatalf("second run: finished %v after %d calls, %d blocked: one block per session and subject", res2.finished, res2.calls, res2.blocked)
			}
			if got := up.count() - before; got != 2 {
				t.Errorf("upstream calls %d, want 2: the repeat and the answer", got)
			}
			if n := r.mon.Status()["notes_injected"].(int64) - notes; n != 1 || countProxy(up.last()) != 2 {
				t.Errorf("notes injected %d (want 1); [proxy] in the last request %d (want 2: the earlier block answer and the note)", n, countProxy(up.last()))
			}
			if c := r.mon.Status()["blocks_capped"]; c != int64(1) {
				t.Errorf("blocks_capped = %v, want 1", c)
			}
		})
	}
}
