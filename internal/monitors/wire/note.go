package wire

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethosagent/tollgate"
)

// AppendNote returns body with note added at the end of the conversation, spliced in place: the
// bytes before and after the inserted text are identical to the input. It is AppendNotes of one note.
func AppendNote(w tollgate.WireFormat, body []byte, note string) ([]byte, error) {
	return AppendNotes(w, body, []string{note})
}

// AppendNotes returns body with every note added at the end of the conversation, in order, scanning the
// body and copying it once however many notes there are. The bytes before and after the inserted text are
// identical to the input.
//
// openai.chat: trailing {"role":"system","content":note} messages. anthropic.messages: extra text
// blocks at the end of the last user message, not in "system", because editing "system" would
// invalidate the provider's prompt-cache prefix on every noted call (ruling R4). It returns
// ErrNoSplice when the body has no such place (no messages, last Anthropic message not a user turn);
// then no note is added.
func AppendNotes(w tollgate.WireFormat, body []byte, notes []string) ([]byte, error) {
	if len(notes) == 0 {
		return body, nil
	}
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
		return appendToArray(body, msgs, joinElems(notes, func(n string) string {
			return `{"role":"system","content":` + jsonString(n) + `}`
		}))
	case tollgate.WireAnthropicMessages:
		return anthropicNotes(body, msgs, notes)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
	}
}

// joinElems renders each note as a JSON array element and joins them with commas.
func joinElems(notes []string, elem func(string) string) string {
	parts := make([]string, len(notes))
	for i, n := range notes {
		parts[i] = elem(n)
	}
	return strings.Join(parts, ",")
}

func anthropicNotes(body []byte, msgs span, notes []string) ([]byte, error) {
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
	blocks := joinElems(notes, func(n string) string { return `{"type":"text","text":` + jsonString(n) + `}` })
	switch body[content.start] {
	case '[':
		return appendToArray(body, content, blocks)
	case '"':
		out := make([]byte, 0, len(body)+len(blocks)+48)
		out = append(out, body[:content.start]...)
		out = append(out, `[{"type":"text","text":`...)
		out = append(out, body[content.start:content.end]...)
		out = append(out, "},"...)
		out = append(out, blocks...)
		out = append(out, ']')
		return append(out, body[content.end:]...), nil
	}
	return nil, fmt.Errorf("%w: unexpected content shape", ErrNoSplice)
}
