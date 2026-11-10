package wire

import "github.com/ethosagent/tollgate"

// Editor is the wire editor as a value: the host injects it into monitors.Options.Editor, because
// archtest H3 forbids the pure monitors package from importing this one. It has no state.
type Editor struct{}

// AppendNote is AppendNote.
func (Editor) AppendNote(w tollgate.WireFormat, body []byte, note string) ([]byte, error) {
	return AppendNote(w, body, note)
}

// BlockResponse is BlockResponse.
func (Editor) BlockResponse(w tollgate.WireFormat, stream bool, msgs []tollgate.Message, note string) (*tollgate.Response, error) {
	return BlockResponse(w, stream, msgs, note)
}
