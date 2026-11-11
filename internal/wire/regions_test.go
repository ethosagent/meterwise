package wire_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/wire"
)

const openaiBody = `{"model":"gpt-4.1","stream":true, "tools":[{"type":"function","function":{"name":"read_file","description":"Reads a file \"safely\"","parameters":{"type":"object"}}},{"type":"function","function":{"name":"no_desc"}}],
 "messages":[{"role":"system","content":"You are <b>helpful</b>"},{"role":"user","content":"go"},
 {"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]},
 {"role":"tool","tool_call_id":"call_1","content":"line1\nline2 é \"q\" <x>"},
 {"role":"assistant","content":"ok","tool_calls":[{"id":"call_2","type":"function","function":{"name":"grep","arguments":"{}"}}]},
 {"role":"tool","tool_call_id":"call_2","content":[{"type":"text","text":"part one"},{"type":"image_url","image_url":{"url":"x"}},{"type":"text","text":"part two"}]}],"temperature":0.2}`

const anthropicBody = `{"model":"claude-sonnet-4-5","max_tokens":10,"system":[{"type":"text","text":"SYS","cache_control":{"type":"ephemeral"}}],
 "tools":[{"name":"bash","description":"Runs a command","input_schema":{"type":"object"}}],
 "messages":[{"role":"user","content":"hi"},
 {"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"bash","input":{"cmd":"ls"}}]},
 {"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":"out one"},{"type":"text","text":"x"}]},
 {"role":"assistant","content":[{"type":"tool_use","id":"tu_2","name":"bash","input":{"cmd":"pwd"}}]},
 {"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_2","content":[{"type":"text","text":"out two","cache_control":{"type":"ephemeral"}}]}]},
 {"role":"assistant","content":[{"type":"tool_use","id":"tu_3","name":"bash","input":{"cmd":"bad"}}]},
 {"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_3","is_error":true,"content":"boom"},{"type":"tool_result","tool_use_id":"tu_3","content":"out three"}]}]}`

func TestToolResultsOpenAI(t *testing.T) {
	rs, err := wire.ToolResults(tollgate.WireOpenAIChat, []byte(openaiBody))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ text, tool, id string }{
		{"line1\nline2 é \"q\" <x>", "read_file", "call_1"},
		{"part one", "grep", "call_2"},
		{"part two", "grep", "call_2"},
	}
	if len(rs) != len(want) {
		t.Fatalf("got %d regions, want %d: %+v", len(rs), len(want), rs)
	}
	for i, w := range want {
		if rs[i].Text != w.text || rs[i].Tool != w.tool || rs[i].ToolCallID != w.id || rs[i].Protected || rs[i].Cached {
			t.Errorf("region %d = %+v, want %+v", i, rs[i], w)
		}
	}
	if rs[0].Msg != 3 || rs[1].Msg != 5 {
		t.Errorf("message indexes = %d, %d, want 3, 5", rs[0].Msg, rs[1].Msg)
	}
}

func TestToolResultsAnthropicMarksCachedAndSkipsErrors(t *testing.T) {
	rs, err := wire.ToolResults(tollgate.WireAnthropicMessages, []byte(anthropicBody))
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 3 {
		t.Fatalf("got %d regions, want 3 (the is_error result is skipped): %+v", len(rs), rs)
	}
	// The last cache_control breakpoint sits in the tool_result of tu_2 (message 4): results at or before it are Cached.
	wantCached := []bool{true, true, false}
	for i, r := range rs {
		if r.Cached != wantCached[i] || r.Protected || r.Tool != "bash" {
			t.Errorf("region %d = %+v, want cached=%v tool=bash", i, r, wantCached[i])
		}
	}
	if rs[2].Text != "out three" || rs[0].Text != "out one" {
		t.Errorf("texts = %q, %q", rs[0].Text, rs[2].Text)
	}
}

func TestToolResultsRejectsOtherWiresAndBadBodies(t *testing.T) {
	if _, err := wire.ToolResults(tollgate.WirePassthrough, []byte(openaiBody)); !errors.Is(err, wire.ErrUnsupportedWire) {
		t.Errorf("passthrough err = %v", err)
	}
	for _, b := range []string{``, `[]`, `{"model":"x"}`, `{"messages":[{"role":"tool","content":"unterminated}]}`, `{"messages":{"a":1}}`} {
		if _, err := wire.ToolResults(tollgate.WireOpenAIChat, []byte(b)); err == nil {
			t.Errorf("body %q: want an error", b)
		}
	}
}

// ReplaceRegions changes only the replaced strings; every byte outside them is identical.
func TestReplaceRegionsPreservesEveryOtherByte(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    tollgate.WireFormat
		body string
	}{{"openai", tollgate.WireOpenAIChat, openaiBody}, {"anthropic", tollgate.WireAnthropicMessages, anthropicBody}} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			rs, err := wire.ToolResults(tc.w, body)
			if err != nil || len(rs) == 0 {
				t.Fatalf("regions: %v %v", rs, err)
			}
			edits := make([]wire.Edit, len(rs))
			for i, r := range rs {
				edits[i] = wire.Edit{Start: r.Start, End: r.End, Text: "[folded <&> \"" + r.Tool + "\"]"}
			}
			out, err := wire.ReplaceRegions(body, edits)
			if err != nil {
				t.Fatal(err)
			}
			var want bytes.Buffer
			at := 0
			for _, r := range rs {
				want.Write(body[at:r.Start])
				want.WriteString(`"[folded <&> \"` + r.Tool + `\"]"`)
				at = r.End
			}
			want.Write(body[at:])
			if !bytes.Equal(out, want.Bytes()) {
				t.Errorf("got %s\nwant %s", out, want.Bytes())
			}
			back, err := wire.ToolResults(tc.w, out)
			if err != nil || len(back) != len(rs) || !strings.HasPrefix(back[0].Text, "[folded <&>") {
				t.Errorf("re-read = %+v, %v", back, err)
			}
		})
	}
}

func TestReplaceRegionsRejectsBadEdits(t *testing.T) {
	body := []byte(openaiBody)
	rs, _ := wire.ToolResults(tollgate.WireOpenAIChat, body)
	a, b := rs[0], rs[1]
	for name, edits := range map[string][]wire.Edit{
		"out of order": {{Start: b.Start, End: b.End}, {Start: a.Start, End: a.End}},
		"overlap":      {{Start: a.Start, End: a.End}, {Start: a.End - 1, End: b.End}},
		"not a string": {{Start: a.Start + 1, End: a.End}},
		"past the end": {{Start: a.Start, End: len(body) + 1}},
		"empty span":   {{Start: a.Start, End: a.Start}},
	} {
		if out, err := wire.ReplaceRegions(body, edits); !errors.Is(err, wire.ErrBadEdit) || out != nil {
			t.Errorf("%s: out %q err %v", name, out, err)
		}
	}
	if out, err := wire.ReplaceRegions(body, nil); err != nil || !bytes.Equal(out, body) {
		t.Errorf("no edits: %v", err)
	}
}

func TestToolDescriptions(t *testing.T) {
	rs, err := wire.ToolDescriptions(tollgate.WireOpenAIChat, []byte(openaiBody))
	if err != nil || len(rs) != 1 || rs[0].Text != `Reads a file "safely"` || rs[0].Tool != "read_file" || rs[0].Msg != 0 {
		t.Fatalf("openai: %+v %v", rs, err)
	}
	rs, err = wire.ToolDescriptions(tollgate.WireAnthropicMessages, []byte(anthropicBody))
	if err != nil || len(rs) != 1 || rs[0].Text != "Runs a command" || rs[0].Tool != "bash" {
		t.Fatalf("anthropic: %+v %v", rs, err)
	}
	if rs, err := wire.ToolDescriptions(tollgate.WireOpenAIChat, []byte(`{"messages":[]}`)); err != nil || rs != nil {
		t.Errorf("no tools: %+v %v", rs, err)
	}
	if _, err := wire.ToolDescriptions(tollgate.WirePassthrough, []byte(openaiBody)); !errors.Is(err, wire.ErrUnsupportedWire) {
		t.Errorf("passthrough err = %v", err)
	}
}

// The spans are the same whether or not the strings were decoded, and Decode round-trips them.
func TestSpansMatchAndDecode(t *testing.T) {
	body := []byte(openaiBody)
	full, _ := wire.ToolResults(tollgate.WireOpenAIChat, body)
	spans, err := wire.ToolResultSpans(tollgate.WireOpenAIChat, body)
	if err != nil || len(spans) != len(full) {
		t.Fatal(err, len(spans))
	}
	for i := range spans {
		if spans[i].Text != "" || spans[i].Start != full[i].Start || spans[i].End != full[i].End || spans[i].Decode(body) != full[i].Text {
			t.Errorf("span %d = %+v vs %+v", i, spans[i], full[i])
		}
	}
	if (wire.Region{Start: 5, End: 3}).Decode(body) != "" {
		t.Error("a bad span decodes to the empty string")
	}
}

// Decode agrees with encoding/json for every escape, including the ones it hands to the general decoder.
func TestDecodeAgreesWithEncodingJSON(t *testing.T) {
	for _, s := range []string{
		"", "plain", "a\nb\tc\rd\be\ff", `quote " slash \ solidus /`, "<&>", "é 日本 🎉", "   ", "ctl \x01 \x1f", "tail\\",
		strings.Repeat("line\n", 500),
	} {
		for _, esc := range []bool{false, true} {
			var raw []byte
			if esc { // the way another client writes it: HTML-escaped, solidus escaped, non-ASCII as \u
				raw = []byte(`"` + strings.NewReplacer("/", `\/`, "<", `<`, "é", `é`, "日", `日`).Replace(jsonOf(s)[1:len(jsonOf(s))-1]) + `"`)
			} else {
				raw = []byte(jsonOf(s))
			}
			var want string
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("%q: %v", raw, err)
			}
			if got := (wire.Region{Start: 0, End: len(raw)}).Decode(raw); got != want {
				t.Errorf("Decode(%q) = %q, want %q", raw, got, want)
			}
		}
	}
	for _, bad := range []string{`"`, `""x`, `"abc`, `"\q"`, `"\"`, `x`} {
		if got := (wire.Region{Start: 0, End: len(bad)}).Decode([]byte(bad)); got != "" && bad != `""x` {
			t.Errorf("Decode(%q) = %q, want the empty string", bad, got)
		}
	}
}

func jsonOf(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
