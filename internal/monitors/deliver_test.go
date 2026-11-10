package monitors_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
	"github.com/ethosagent/meterwise/internal/monitors/wire"
)

// S2: a session's one note or block per subject is used up only when it is delivered: the note spliced
// into a body that goes upstream, the block returned to the chain. An editor error, or a chain that
// stops waiting (ctx done) after the edit, leaves the subject for the next call.

// flakyEditor is wire.Editor that, while armed, fails or cancels the call's context on its next edit.
type flakyEditor struct {
	wire.Editor
	fail   bool               // the next edit returns an error
	cancel context.CancelFunc // the next edit cancels the chain's context (then succeeds)
}

func (e *flakyEditor) trip() error {
	if e.fail {
		e.fail = false
		return errors.New("flaky editor")
	}
	if e.cancel != nil {
		e.cancel()
		e.cancel = nil
	}
	return nil
}

func (e *flakyEditor) AppendNotes(w tollgate.WireFormat, body []byte, notes []string) ([]byte, error) {
	if err := e.trip(); err != nil {
		return nil, err
	}
	return e.Editor.AppendNotes(w, body, notes)
}

func (e *flakyEditor) BlockResponse(w tollgate.WireFormat, stream bool, text string) (*tollgate.Response, error) {
	if err := e.trip(); err != nil {
		return nil, err
	}
	return e.Editor.BlockResponse(w, stream, text)
}

// attempt runs one Before of call 4 (the repeat) with ctx, returning the block response and the body.
func attempt(ctx context.Context, t *testing.T, h *monitors.Hook, mode contract.MonitorMode) (*tollgate.Response, *tollgate.Call) {
	t.Helper()
	c := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(mode), 4, weatherTranscript(3))
	resp, err := h.Before(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	return resp, c
}

func TestUndeliveredNoteDoesNotUseUpTheSubject(t *testing.T) {
	for _, name := range []string{"editor error", "chain gone after the splice"} {
		t.Run(name, func(t *testing.T) {
			ed := &flakyEditor{}
			h := newMon(monitors.Options{Editor: ed})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "editor error" {
				ed.fail = true
			} else {
				ed.cancel = cancel
			}
			if _, c := attempt(ctx, t, h, contract.ModeNote); countProxy(c.Body) != 0 {
				t.Fatalf("first attempt delivered a note: %s", c.Body)
			}
			_, c := attempt(context.Background(), t, h, contract.ModeNote)
			vs, notes := verdictsOf(c)
			if notes != 1 || countProxy(c.Body) != 1 || len(vs) != 1 || vs[0].Action != contract.ModeNote {
				t.Fatalf("retry: notes %d verdicts %+v", notes, vs)
			}
			if _, c := attempt(context.Background(), t, h, contract.ModeNote); countProxy(c.Body) != 0 {
				t.Error("a delivered note must use up the subject")
			}
		})
	}
}

func TestUndeliveredBlockDoesNotUseUpTheSubject(t *testing.T) {
	for _, name := range []string{"editor error", "chain gone after the block was built"} {
		t.Run(name, func(t *testing.T) {
			ed := &flakyEditor{}
			h := newMon(monitors.Options{Editor: ed})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if name == "editor error" {
				ed.fail = true
			} else {
				ed.cancel = cancel
			}
			if resp, _ := attempt(ctx, t, h, contract.ModeBlock); resp != nil {
				t.Fatal("first attempt delivered a block")
			}
			resp, c := attempt(context.Background(), t, h, contract.ModeBlock)
			if vs, _ := verdictsOf(c); resp == nil || len(vs) != 1 || vs[0].Action != contract.ModeBlock {
				t.Fatalf("retry was not blocked: %+v status %v", vs, h.Status())
			}
			if resp, _ := attempt(context.Background(), t, h, contract.ModeBlock); resp != nil {
				t.Error("a delivered block must use up the subject (cap: one per session and subject)")
			}
		})
	}
}
