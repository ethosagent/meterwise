package wire

import "github.com/ethosagent/tollgate"

// Editor is the wire editor as a value: the host injects it into monitors.Options.Editor, because
// archtest H3 forbids the pure monitors package from importing this one. It has no state.
type Editor struct{}

// AppendNotes is AppendNotes.
func (Editor) AppendNotes(w tollgate.WireFormat, body []byte, notes []string) ([]byte, error) {
	return AppendNotes(w, body, notes)
}

// BlockResponse is BlockResponse.
func (Editor) BlockResponse(w tollgate.WireFormat, stream bool, text string) (*tollgate.Response, error) {
	return BlockResponse(w, stream, text)
}
