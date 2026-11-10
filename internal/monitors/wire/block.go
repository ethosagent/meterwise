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

// BlockResponse builds the answer a blocking monitor returns instead of calling the upstream: one
// terminal assistant turn holding only text (no tool call, finish_reason "stop" / stop_reason
// "end_turn"), so an agent loop ends its turn on it instead of running a tool again. The text is built
// by the caller from the current request only; nothing is replayed. stream selects whole SSE frames
// (Content-Type text/event-stream) over one JSON body. Usage is zero.
func BlockResponse(w tollgate.WireFormat, stream bool, text string) (*tollgate.Response, error) {
	b, ok := builders[w]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedWire, w)
	}
	build := b.json
	if stream {
		build = b.stream
	}
	body, err := build(text)
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

// builder makes the body of a block in one wire: one JSON object, or the whole SSE stream.
type builder struct {
	json, stream func(text string) ([]byte, error)
}

var builders = map[tollgate.WireFormat]builder{
	tollgate.WireOpenAIChat:        {openAIJSON, openAIStream},
	tollgate.WireAnthropicMessages: {anthropicJSON, anthropicStream},
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
