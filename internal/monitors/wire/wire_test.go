package wire_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/monitors/wire"
)

const note = "[proxy] You already ran this exact call at step 3 <&>."

// splitAround checks out is in plus exactly the inserted text, and returns what was inserted.
func inserted(t *testing.T, in, out []byte) string {
	t.Helper()
	p := 0
	for p < len(in) && p < len(out) && in[p] == out[p] {
		p++
	}
	s := 0
	for s < len(in)-p && in[len(in)-1-s] == out[len(out)-1-s] {
		s++
	}
	if p+s < len(in) {
		t.Fatalf("bytes outside the splice differ: common prefix %d suffix %d of %d", p, s, len(in))
	}
	return string(out[p : len(out)-s])
}

func TestAppendNoteTable(t *testing.T) {
	oa := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"},{"role":"tool","tool_call_id":"c1","content":"r"}],"stream":true}`
	anStr := `{"model":"claude","system":"S","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":"again"}],"max_tokens":9}`
	anArr := `{"model":"claude","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"r"}]}], "max_tokens":9}`
	cases := []struct {
		name    string
		w       tollgate.WireFormat
		body    string
		wantIns string
		wantErr error
	}{
		{"openai appends a trailing system message", tollgate.WireOpenAIChat, oa, `,{"role":"system","content":"` + note + `"}`, nil},
		{"openai empty messages", tollgate.WireOpenAIChat, `{"messages":[ ]}`, `{"role":"system","content":"` + note + `"}`, nil},
		{"anthropic string content becomes two blocks", tollgate.WireAnthropicMessages, anStr,
			"FULL:" + `{"model":"claude","system":"S","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"yo"},{"role":"user","content":[{"type":"text","text":"again"},{"type":"text","text":"` + note + `"}]}],"max_tokens":9}`, nil},
		{"anthropic block content gets one more block", tollgate.WireAnthropicMessages, anArr, `,{"type":"text","text":"` + note + `"}`, nil},
		{"anthropic last turn is the assistant", tollgate.WireAnthropicMessages, `{"messages":[{"role":"assistant","content":"x"}]}`, "", wire.ErrNoSplice},
		{"no messages", tollgate.WireOpenAIChat, `{"model":"x"}`, "", wire.ErrNoSplice},
		{"passthrough", tollgate.WirePassthrough, oa, "", wire.ErrUnsupportedWire},
		{"not json", tollgate.WireOpenAIChat, `nope`, "", wire.ErrNoSplice},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := wire.AppendNote(tc.w, []byte(tc.body), note)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(out) {
				t.Fatalf("result is not valid JSON: %s", out)
			}
			if full, ok := strings.CutPrefix(tc.wantIns, "FULL:"); ok {
				if string(out) != full {
					t.Errorf("got  %s\nwant %s", out, full)
				}
			} else if got := inserted(t, []byte(tc.body), out); got != tc.wantIns {
				t.Errorf("inserted %q, want %q", got, tc.wantIns)
			}
			if tc.w == tollgate.WireAnthropicMessages && !strings.Contains(string(out), `"system":"S"`) && strings.Contains(tc.body, `"system":"S"`) {
				t.Error("system must be untouched")
			}
		})
	}
}

func TestAppendNoteTwiceKeepsBothAndValid(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":"a"}]}`)
	one, _ := wire.AppendNote(tollgate.WireOpenAIChat, body, "[proxy] one")
	two, err := wire.AppendNote(tollgate.WireOpenAIChat, one, "[proxy] two")
	if err != nil || !json.Valid(two) || bytes.Count(two, []byte("[proxy]")) != 2 {
		t.Fatalf("two notes: %v %s", err, two)
	}
}

var turn = []tollgate.Message{
	{Role: tollgate.RoleUser, Text: "q"},
	{Role: tollgate.RoleAssistant, Text: "let me read it", ToolCalls: []tollgate.ToolCall{{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"/a.go"}`)}}},
}

func TestBlockResponseOpenAIJSON(t *testing.T) {
	r, err := wire.BlockResponse(tollgate.WireOpenAIChat, false, turn, note)
	if err != nil || r.Status != 200 || r.Header["Content-Type"][0] != "application/json" {
		t.Fatalf("%v %+v", err, r)
	}
	var v struct {
		Choices []struct {
			Message struct {
				Role      string
				Content   string
				ToolCalls []struct {
					ID       string
					Function struct{ Name, Arguments string }
				} `json:"tool_calls"`
			}
			FinishReason string `json:"finish_reason"`
		}
		Usage struct {
			TotalTokens int `json:"total_tokens"`
		}
	}
	if err := json.Unmarshal(r.Body, &v); err != nil {
		t.Fatal(err)
	}
	m := v.Choices[0].Message
	if m.Role != "assistant" || m.Content != "let me read it\n\n"+note || v.Choices[0].FinishReason != "tool_calls" || v.Usage.TotalTokens != 0 {
		t.Errorf("%+v", v)
	}
	if len(m.ToolCalls) != 1 || m.ToolCalls[0].ID != "call_1" || m.ToolCalls[0].Function.Name != "read_file" || m.ToolCalls[0].Function.Arguments != `{"path":"/a.go"}` {
		t.Errorf("tool calls %+v", m.ToolCalls)
	}
}

func dataFrames(t *testing.T, body []byte) (events []string, datas []string) {
	t.Helper()
	if !bytes.HasSuffix(body, []byte("\n\n")) {
		t.Fatalf("stream must end on a blank line: %q", body)
	}
	for _, f := range strings.Split(strings.TrimSuffix(string(body), "\n\n"), "\n\n") {
		var ev, data string
		for _, ln := range strings.Split(f, "\n") {
			switch {
			case strings.HasPrefix(ln, "event: "):
				ev = strings.TrimPrefix(ln, "event: ")
			case strings.HasPrefix(ln, "data: "):
				data = strings.TrimPrefix(ln, "data: ")
			default:
				t.Fatalf("bad frame line %q", ln)
			}
		}
		events, datas = append(events, ev), append(datas, data)
	}
	return events, datas
}

func TestBlockResponseOpenAIStream(t *testing.T) {
	r, err := wire.BlockResponse(tollgate.WireOpenAIChat, true, turn, note)
	if err != nil || r.Header["Content-Type"][0] != "text/event-stream" {
		t.Fatalf("%v %+v", err, r)
	}
	_, datas := dataFrames(t, r.Body)
	if len(datas) != 4 || datas[3] != "[DONE]" {
		t.Fatalf("frames = %q", datas)
	}
	var text, args, finish string
	for _, d := range datas[:3] {
		var c struct {
			Choices []struct {
				Delta struct {
					Content   string
					ToolCalls []struct {
						Index    int
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
				}
				FinishReason *string `json:"finish_reason"`
			}
		}
		if err := json.Unmarshal([]byte(d), &c); err != nil {
			t.Fatalf("frame %q: %v", d, err)
		}
		text += c.Choices[0].Delta.Content
		for _, tc := range c.Choices[0].Delta.ToolCalls {
			args += tc.Function.Arguments
		}
		if c.Choices[0].FinishReason != nil {
			finish = *c.Choices[0].FinishReason
		}
	}
	if text != "let me read it\n\n"+note || args != `{"path":"/a.go"}` || finish != "tool_calls" {
		t.Errorf("text %q args %q finish %q", text, args, finish)
	}
}

func TestBlockResponseAnthropic(t *testing.T) {
	r, err := wire.BlockResponse(tollgate.WireAnthropicMessages, false, turn, note)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type, Text, ID, Name string
			Input                json.RawMessage
		}
	}
	if err := json.Unmarshal(r.Body, &v); err != nil || v.StopReason != "tool_use" || len(v.Content) != 2 {
		t.Fatalf("%v %+v", err, v)
	}
	if v.Content[0].Text != "let me read it\n\n"+note || v.Content[1].Type != "tool_use" || string(v.Content[1].Input) != `{"path":"/a.go"}` {
		t.Errorf("%+v", v.Content)
	}
	rs, err := wire.BlockResponse(tollgate.WireAnthropicMessages, true, turn, note)
	if err != nil {
		t.Fatal(err)
	}
	events, datas := dataFrames(t, rs.Body)
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if strings.Join(events, ",") != strings.Join(want, ",") {
		t.Fatalf("events = %v", events)
	}
	for i, d := range datas {
		var x struct{ Type string }
		if json.Unmarshal([]byte(d), &x) != nil || x.Type != events[i] {
			t.Errorf("frame %d data %q does not carry type %q", i, d, events[i])
		}
	}
	if !strings.Contains(datas[5], `"partial_json":"{\"path\":\"/a.go\"}"`) {
		t.Errorf("tool args delta: %s", datas[5])
	}
}

func TestBlockResponseNoTurnOrWire(t *testing.T) {
	if _, err := wire.BlockResponse(tollgate.WireOpenAIChat, false, turn[:1], note); !errors.Is(err, wire.ErrNoTurn) {
		t.Errorf("no assistant turn: %v", err)
	}
	if _, err := wire.BlockResponse(tollgate.WirePassthrough, false, turn, note); !errors.Is(err, wire.ErrUnsupportedWire) {
		t.Errorf("passthrough: %v", err)
	}
	plain := []tollgate.Message{{Role: tollgate.RoleAssistant, Text: "done"}}
	r, err := wire.BlockResponse(tollgate.WireOpenAIChat, false, plain, note)
	if err != nil || !strings.Contains(string(r.Body), `"finish_reason":"stop"`) || strings.Contains(string(r.Body), "tool_calls") {
		t.Errorf("text-only turn: %v %s", err, r.Body)
	}
}
