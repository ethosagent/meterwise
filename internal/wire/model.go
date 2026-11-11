package wire

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// SetModel returns body with the top-level "model" string replaced by model. It is a byte splice: every byte outside
// the old string value is copied unchanged (the router changes one field and nothing else, ADR 0029). It returns
// ErrNoSplice when the body has no top-level string "model", and ErrUnsupportedWire for a wire it does not know.
func SetModel(w tollgate.WireFormat, body []byte, model string) ([]byte, error) {
	if !supported(w) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
	}
	fields, err := objectFields(body)
	if err != nil {
		return nil, err
	}
	m, ok := fields["model"]
	if !ok || body[m.start] != '"' {
		return nil, fmt.Errorf("%w: no model string", ErrNoSplice)
	}
	enc := jsonString(model)
	out := make([]byte, 0, len(body)-(m.end-m.start)+len(enc))
	out = append(out, body[:m.start]...)
	out = append(out, enc...)
	return append(out, body[m.end:]...), nil
}

func supported(w tollgate.WireFormat) bool {
	return w == tollgate.WireOpenAIChat || w == tollgate.WireAnthropicMessages
}

// Features reads what the router needs to know about a request before it may swap the model: how many tool
// definitions it carries, whether it asks for a JSON schema (OpenAI response_format other than text, Anthropic
// output_format or output_config.format), whether any message holds an image part, and whether it sets a reasoning
// control (OpenAI reasoning_effort or reasoning, Anthropic thinking that is not disabled, output_config.effort).
// It reads the body in place (no re-marshal) and reports ErrNoSplice for a body it cannot walk.
func Features(w tollgate.WireFormat, body []byte) (contract.RequestFeatures, error) {
	var f contract.RequestFeatures
	if !supported(w) {
		return f, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
	}
	fields, err := objectFields(body)
	if err != nil {
		return f, err
	}
	for _, key := range [...]string{"tools", "functions"} {
		if s, ok := fields[key]; ok && body[s.start] == '[' {
			elems, err := arrayElems(body[s.start:s.end])
			if err != nil {
				return f, err
			}
			f.Tools += len(elems)
		}
	}
	if s, ok := fields["messages"]; ok {
		f.Vision = hasImagePart(body[s.start:s.end])
	}
	f.JSONSchema, f.Reasoning = structuredAndReasoning(w, body, fields)
	return f, nil
}

// structuredAndReasoning reads the small control fields of the request.
func structuredAndReasoning(w tollgate.WireFormat, body []byte, fields map[string]span) (schema, reasoning bool) {
	raw := func(key string) []byte {
		if s, ok := fields[key]; ok {
			if v := bytes.TrimSpace(body[s.start:s.end]); !bytes.Equal(v, []byte("null")) {
				return v
			}
		}
		return nil
	}
	if w == tollgate.WireOpenAIChat {
		if v := raw("response_format"); v != nil {
			var rf struct {
				Type string `json:"type"`
			}
			// Anything but a plain text format asks for structured output; an unreadable one is treated the same (deny).
			schema = json.Unmarshal(v, &rf) != nil || rf.Type != "text"
		}
		return schema, raw("reasoning_effort") != nil || raw("reasoning") != nil
	}
	schema = raw("output_format") != nil
	if v := raw("output_config"); v != nil {
		var oc struct {
			Format json.RawMessage `json:"format"`
			Effort json.RawMessage `json:"effort"`
		}
		if json.Unmarshal(v, &oc) != nil {
			return true, true // unreadable: deny both
		}
		schema = schema || (len(oc.Format) > 0 && string(oc.Format) != "null")
		reasoning = len(oc.Effort) > 0 && string(oc.Effort) != "null"
	}
	if v := raw("thinking"); v != nil {
		var th struct {
			Type string `json:"type"`
		}
		reasoning = reasoning || json.Unmarshal(v, &th) != nil || th.Type != "disabled"
	}
	return schema, reasoning
}

// hasImagePart reports whether any object in the messages array has "type" set to an image part type. It walks the
// bytes once without allocating: a string followed by a colon is always a key, so text that merely mentions the
// word cannot match (its quotes are escaped).
func hasImagePart(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '"' {
			continue
		}
		end, err := skipString(b, i)
		if err != nil {
			return false
		}
		key := b[i+1 : end-1]
		i = end - 1
		if string(key) != "type" {
			continue
		}
		j := skipSpace(b, end)
		if j >= len(b) || b[j] != ':' {
			continue
		}
		j = skipSpace(b, j+1)
		if j >= len(b) || b[j] != '"' {
			continue
		}
		vend, err := skipString(b, j)
		if err != nil {
			return false
		}
		switch string(b[j+1 : vend-1]) {
		case "image_url", "image", "input_image":
			return true
		}
		i = vend - 1
	}
	return false
}
