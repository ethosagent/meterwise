package monitors_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
)

// K1: only the queued stream notes that were spliced leave the queue; a splice that fails leaves them
// queued for the next call. K2: a session's one note or block per subject is reserved before the edit, so
// two concurrent Befores of the session deliver it once.

func TestQueuedStreamNoteStaysQueuedWhenItsSpliceFails(t *testing.T) {
	ed := &flakyEditor{}
	h := newMon(monitors.Options{Editor: ed})
	streamed(t, h, "s1", 3, doubtful) // queues a second_guessing note for s1

	next := func(calls int) *tollgate.Call {
		c := before(t, tollgate.WireOpenAIChat, fakeRing{}, sgAttrs(), calls, weatherTranscript(1))
		c.Session.ID = "s1"
		h.Before(bg, c)
		return c
	}
	ed.fail = true
	if c := next(4); countProxy(c.Body) != 0 {
		t.Fatalf("the failed splice delivered a note: %s", c.Body)
	}
	if h.Status()["notes_dropped"] != int64(1) {
		t.Errorf("the failed splice is counted: %v", h.Status())
	}
	if c := next(5); countProxy(c.Body) != 1 || !strings.Contains(string(c.Body), "reversed itself") {
		t.Fatalf("the undelivered note must still be queued: %s", c.Body)
	}
	if c := next(6); countProxy(c.Body) != 0 {
		t.Error("a delivered queued note leaves the queue")
	}
	again := streamed(t, h, "s1", 3, doubtful)
	if vs, _ := verdictsOf(again); len(vs) != 1 || vs[0].Action != contract.ModeObserve {
		t.Errorf("a delivered stream note uses up its subject: %+v", vs)
	}
}

// gateEditor holds the first edit it gets until release is closed, so a second Before runs meanwhile.
type gateEditor struct {
	flakyEditor
	first   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (e *gateEditor) gate() {
	if e.first.CompareAndSwap(false, true) {
		close(e.entered)
		<-e.release
	}
}

func (e *gateEditor) AppendNote(w tollgate.WireFormat, body []byte, text string) ([]byte, error) {
	e.gate()
	return e.Editor.AppendNote(w, body, text)
}

func (e *gateEditor) BlockResponse(w tollgate.WireFormat, stream bool, text string) (*tollgate.Response, error) {
	e.gate()
	return e.Editor.BlockResponse(w, stream, text)
}

func TestConcurrentBeforesOfOneSessionDeliverTheSubjectOnce(t *testing.T) {
	for _, mode := range []contract.MonitorMode{contract.ModeNote, contract.ModeBlock} {
		t.Run(string(mode), func(t *testing.T) {
			ed := &gateEditor{entered: make(chan struct{}), release: make(chan struct{})}
			h := newMon(monitors.Options{Editor: ed})
			var a *tollgate.Call
			var aResp *tollgate.Response
			done := make(chan struct{})
			go func() {
				defer close(done)
				a = before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(mode), 4, weatherTranscript(3))
				aResp, _ = h.Before(bg, a)
			}()
			<-ed.entered // A is inside its edit, holding the subject
			b := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(mode), 4, weatherTranscript(3))
			bResp, _ := h.Before(bg, b)
			close(ed.release)
			<-done
			// a block capped by the first one degrades to a note (B1), so only blocks count in block mode
			delivered := func(c *tollgate.Call, r *tollgate.Response) int {
				if mode == contract.ModeBlock {
					if r != nil {
						return 1
					}
					return 0
				}
				return countProxy(c.Body)
			}
			if got := delivered(a, aResp) + delivered(b, bResp); got != 1 {
				t.Errorf("the subject was delivered %d times, want once (a %v, b %v)", got, delivered(a, aResp), delivered(b, bResp))
			}
		})
	}
}
