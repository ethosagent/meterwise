package wire

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/ethosagent/tollgate"
)

// AppendNote returns body with note added at the end of the conversation, spliced in place: the
// bytes before and after the inserted text are identical to the input.
//
// openai.chat: a trailing {"role":"system","content":note} message. anthropic.messages: an extra text
// block at the end of the last user message, not in "system", because editing "system" would
// invalidate the provider's prompt-cache prefix on every noted call (ruling R4). It returns
// ErrNoSplice when the body has no such place (no messages, last Anthropic message not a user turn).
func AppendNote(w tollgate.WireFormat, body []byte, note string) ([]byte, error) {
	fields, err := objectFields(body)
	if err != nil {
		return nil, err
	}
	msgs, ok := fields["messages"]
	if !ok {
		return nil, fmt.Errorf("%w: no messages", ErrNoSplice)
	}
	switch w {
	case tollgate.WireOpenAIChat:
		return appendToArray(body, msgs, `{"role":"system","content":`+jsonString(note)+`}`)
	case tollgate.WireAnthropicMessages:
		return anthropicNote(body, msgs, note)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
	}
}

func anthropicNote(body []byte, msgs span, note string) ([]byte, error) {
	elems, err := arrayElems(body[msgs.start:msgs.end])
	if err != nil || len(elems) == 0 {
		return nil, fmt.Errorf("%w: no last message", ErrNoSplice)
	}
	last := span{msgs.start + elems[len(elems)-1].start, msgs.start + elems[len(elems)-1].end}
	f, err := objectFields(body[last.start:last.end])
	if err != nil {
		return nil, err
	}
	var role string
	if r, ok := f["role"]; !ok || json.Unmarshal(body[last.start+r.start:last.start+r.end], &role) != nil || role != "user" {
		return nil, fmt.Errorf("%w: last message is not a user turn", ErrNoSplice)
	}
	c, ok := f["content"]
	if !ok {
		return nil, fmt.Errorf("%w: no content", ErrNoSplice)
	}
	content := span{last.start + c.start, last.start + c.end}
	block := `{"type":"text","text":` + jsonString(note) + `}`
	switch body[content.start] {
	case '[':
		return appendToArray(body, content, block)
	case '"':
		orig := bytes.Clone(body[content.start:content.end])
		repl := `[{"type":"text","text":` + string(orig) + `},` + block + `]`
		out := make([]byte, 0, len(body)+len(repl))
		out = append(out, body[:content.start]...)
		out = append(out, repl...)
		return append(out, body[content.end:]...), nil
	}
	return nil, fmt.Errorf("%w: unexpected content shape", ErrNoSplice)
}
