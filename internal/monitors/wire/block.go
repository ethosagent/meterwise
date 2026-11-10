package wire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ethosagent/tollgate"
)

// blockModel names the model on a built response; the upstream was never called.
const blockModel = "meterwise-block"

// BlockResponse builds the answer a blocking monitor returns instead of calling the upstream: the
// last assistant message of msgs (the turn to replay, taken from the current request, never from
// memory) with note appended to its text. stream selects whole SSE frames (Content-Type
// text/event-stream) over one JSON body. Usage is zero. It returns ErrNoTurn when msgs holds no
// assistant message.
func BlockResponse(w tollgate.WireFormat, stream bool, msgs []tollgate.Message, note string) (*tollgate.Response, error) {
	var turn *tollgate.Message
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == tollgate.RoleAssistant {
			turn = &msgs[i]
			break
		}
	}
	if turn == nil {
		return nil, ErrNoTurn
	}
	text := turn.Text
	if text != "" {
		text += "\n\n"
	}
	text += note
	var body []byte
	var err error
	switch {
	case w == tollgate.WireOpenAIChat && stream:
		body, err = openAIStream(text, turn.ToolCalls)
	case w == tollgate.WireOpenAIChat:
		body, err = openAIJSON(text, turn.ToolCalls)
	case w == tollgate.WireAnthropicMessages && stream:
		body, err = anthropicStream(text, turn.ToolCalls)
	case w == tollgate.WireAnthropicMessages:
		body, err = anthropicJSON(text, turn.ToolCalls)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
	}
	if err != nil {
		return nil, err
	}
	ct := "application/json"
	hdr := map[string][]string{}
	if stream {
		ct = "text/event-stream"
		hdr["Cache-Control"] = []string{"no-cache"}
	}
	hdr["Content-Type"] = []string{ct}
	return &tollgate.Response{Status: 200, Header: hdr, Body: body}, nil
}

// argsOrEmpty returns tool arguments as JSON text, "{}" when empty or not valid JSON.
func argsOrEmpty(a json.RawMessage) string {
	if len(bytes.TrimSpace(a)) == 0 || !json.Valid(a) {
		return "{}"
	}
	return string(a)
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func sse(b *strings.Builder, event string, v any) error {
	data, err := marshal(v)
	if err != nil {
		return err
	}
	if event != "" {
		b.WriteString("event: " + event + "\n")
	}
	b.WriteString("data: " + string(data) + "\n\n")
	return nil
}

type obj = map[string]any
