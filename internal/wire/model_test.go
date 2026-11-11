package wire_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wire"
)

func TestSetModelReplacesOnlyTheModelString(t *testing.T) {
	cases := []struct {
		name string
		w    tollgate.WireFormat
		to   string
		body string
		want string
	}{
		{"openai", tollgate.WireOpenAIChat, "gpt-4.1-mini",
			`{"messages":[{"role":"user","content":"my \"model\":\"x\""}],"model": "gpt-4.1" ,"stream":true}`,
			`{"messages":[{"role":"user","content":"my \"model\":\"x\""}],"model": "gpt-4.1-mini" ,"stream":true}`},
		{"anthropic dated id", tollgate.WireAnthropicMessages, "claude-haiku-4-5",
			`{"model":"claude-sonnet-4-5-20250929","max_tokens":9,"messages":[]}`,
			`{"model":"claude-haiku-4-5","max_tokens":9,"messages":[]}`},
		{"a nested model is not the request's", tollgate.WireOpenAIChat, "b",
			`{"tools":[{"model":"keep"}],"model":"a"}`, `{"tools":[{"model":"keep"}],"model":"b"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := wire.SetModel(tc.w, []byte(tc.body), tc.to)
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tc.want {
				t.Errorf("got  %s\nwant %s", out, tc.want)
			}
		})
	}
}

func TestSetModelErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    tollgate.WireFormat
		body string
		want error
	}{
		{"no model", tollgate.WireOpenAIChat, `{"messages":[]}`, wire.ErrNoSplice},
		{"model is not a string", tollgate.WireOpenAIChat, `{"model":7}`, wire.ErrNoSplice},
		{"not json", tollgate.WireOpenAIChat, `nope`, wire.ErrNoSplice},
		{"passthrough", tollgate.WirePassthrough, `{"model":"a"}`, wire.ErrUnsupportedWire},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := wire.SetModel(tc.w, []byte(tc.body), "b"); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSetModelEscapesTheTarget(t *testing.T) {
	out, err := wire.SetModel(tollgate.WireOpenAIChat, []byte(`{"model":"a"}`), `b"c<`)
	if err != nil || string(out) != `{"model":"b\"c<"}` {
		t.Fatalf("out %s err %v", out, err)
	}
}

func TestFeatures(t *testing.T) {
	cases := []struct {
		name string
		w    tollgate.WireFormat
		body string
		want contract.RequestFeatures
	}{
		{"openai plain", tollgate.WireOpenAIChat, `{"model":"m","messages":[{"role":"user","content":"hi"}]}`, contract.RequestFeatures{}},
		{"openai tools", tollgate.WireOpenAIChat, `{"model":"m","tools":[{"type":"function"},{"type":"function"}],"messages":[]}`, contract.RequestFeatures{Tools: 2}},
		{"openai legacy functions plus tools", tollgate.WireOpenAIChat, `{"functions":[{}],"tools":[{}],"messages":[]}`, contract.RequestFeatures{Tools: 2}},
		{"openai empty tools", tollgate.WireOpenAIChat, `{"tools":[],"messages":[]}`, contract.RequestFeatures{}},
		{"json_schema", tollgate.WireOpenAIChat, `{"response_format":{"type":"json_schema","json_schema":{}},"messages":[]}`, contract.RequestFeatures{JSONSchema: true}},
		{"json_object", tollgate.WireOpenAIChat, `{"response_format":{"type":"json_object"},"messages":[]}`, contract.RequestFeatures{JSONSchema: true}},
		{"response_format text", tollgate.WireOpenAIChat, `{"response_format":{"type":"text"},"messages":[]}`, contract.RequestFeatures{}},
		{"response_format null", tollgate.WireOpenAIChat, `{"response_format":null,"messages":[]}`, contract.RequestFeatures{}},
		{"image_url part", tollgate.WireOpenAIChat, `{"messages":[{"role":"user","content":[{"type":"text","text":"x"},{"type": "image_url","image_url":{"url":"u"}}]}]}`, contract.RequestFeatures{Vision: true}},
		{"text that names image_url is not an image", tollgate.WireOpenAIChat, `{"messages":[{"role":"user","content":"{\"type\":\"image_url\"}"}]}`, contract.RequestFeatures{}},
		{"reasoning_effort", tollgate.WireOpenAIChat, `{"reasoning_effort":"high","messages":[]}`, contract.RequestFeatures{Reasoning: true}},
		{"reasoning_effort null", tollgate.WireOpenAIChat, `{"reasoning_effort":null,"messages":[]}`, contract.RequestFeatures{}},
		{"reasoning object", tollgate.WireOpenAIChat, `{"reasoning":{"effort":"low"},"messages":[]}`, contract.RequestFeatures{Reasoning: true}},
		{"anthropic tools", tollgate.WireAnthropicMessages, `{"tools":[{"name":"a"}],"messages":[]}`, contract.RequestFeatures{Tools: 1}},
		{"anthropic image", tollgate.WireAnthropicMessages, `{"messages":[{"role":"user","content":[{"type":"image","source":{}}]}]}`, contract.RequestFeatures{Vision: true}},
		{"anthropic thinking", tollgate.WireAnthropicMessages, `{"thinking":{"type":"enabled","budget_tokens":1024},"messages":[]}`, contract.RequestFeatures{Reasoning: true}},
		{"anthropic thinking disabled", tollgate.WireAnthropicMessages, `{"thinking":{"type":"disabled"},"messages":[]}`, contract.RequestFeatures{}},
		{"anthropic output_format", tollgate.WireAnthropicMessages, `{"output_format":{"type":"json_schema"},"messages":[]}`, contract.RequestFeatures{JSONSchema: true}},
		{"anthropic output_config", tollgate.WireAnthropicMessages, `{"output_config":{"format":{"type":"json_schema"},"effort":"high"},"messages":[]}`, contract.RequestFeatures{JSONSchema: true, Reasoning: true}},
		{"anthropic output_config without format", tollgate.WireAnthropicMessages, `{"output_config":{},"messages":[]}`, contract.RequestFeatures{}},
		{"openai fields do not apply to anthropic", tollgate.WireAnthropicMessages, `{"response_format":{"type":"json_schema"},"messages":[]}`, contract.RequestFeatures{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := wire.Features(tc.w, []byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestFeaturesErrors(t *testing.T) {
	if _, err := wire.Features(tollgate.WirePassthrough, []byte(`{}`)); !errors.Is(err, wire.ErrUnsupportedWire) {
		t.Errorf("passthrough: %v", err)
	}
	if _, err := wire.Features(tollgate.WireOpenAIChat, []byte(`nope`)); !errors.Is(err, wire.ErrNoSplice) {
		t.Errorf("not json: %v", err)
	}
	if _, err := wire.Features(tollgate.WireOpenAIChat, []byte(`{"tools":[{`)); err == nil {
		t.Error("a truncated body must be an error")
	}
}

func TestFeaturesAllocatesLittleOnALargeBody(t *testing.T) {
	var sb strings.Builder
	sb.WriteString(`{"model":"m","messages":[`)
	for i := range 400 {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"role":"tool","tool_call_id":"c","content":"` + strings.Repeat("line of output ", 20) + `"}`)
	}
	sb.WriteString(`],"tools":[{"type":"function"}]}`)
	body := []byte(sb.String())
	allocs := testing.AllocsPerRun(20, func() { _, _ = wire.Features(tollgate.WireOpenAIChat, body) })
	if allocs > 20 {
		t.Errorf("Features allocated %.0f times on a %d byte body; it should not scale with messages", allocs, len(body))
	}
}
