package monitors_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/monitors"
)

func okMetric() tollgate.Metric { return tollgate.Metric{Finish: tollgate.FinishOK, Status: 200} }

const readArgs = `{"path":"a.go"}`

// sgRing is a read, then a grep: a stream that reads the same file again with doubt is a reversal.
func sgRing() fakeRing {
	return fakeRing{
		{Index: 1, Tools: []string{"read_file"}, ArgsHashes: []uint64{contract.ToolArgsHash("read_file", []byte(readArgs))}},
		{Index: 2, Tools: []string{"grep"}, ArgsHashes: []uint64{2}},
	}
}

// readAgain is the stream event of a tool call that reads a.go again.
func readAgain() tollgate.Event {
	return tollgate.Event{Kind: tollgate.EventToolCallDelta, ToolCall: &tollgate.ToolCallDelta{Index: 0, Name: "read_file", Arguments: readArgs}}
}

func sgAttrs() map[string]string {
	return attr(func(c *contract.MonitorConfig) { c.SecondGuessing.Mode = contract.ModeNote })
}

// streamed plays one streamed response that reads a.go again through Before, OnEvent and After.
func streamed(t *testing.T, h *monitors.Hook, session string, calls int, text string) *tollgate.Call {
	t.Helper()
	c := before(t, tollgate.WireOpenAIChat, sgRing(), sgAttrs(), calls, []tollgate.Message{user("go")})
	c.Session.ID, c.Stream = session, true
	h.Before(bg, c)
	h.OnEvent(bg, c, tollgate.Event{Kind: tollgate.EventTextDelta, Text: text})
	h.OnEvent(bg, c, readAgain())
	h.After(bg, c, okMetric())
	return c
}

const doubtful = "No wait, that is not right. On second thought, let me reconsider."

func TestStreamMonitorRecordsWhereItFiredAndNotesTheNextCall(t *testing.T) {
	h := newMon(monitors.Options{})
	fired := streamed(t, h, "s1", 3, doubtful)
	vs, notes := verdictsOf(fired)
	if len(vs) != 1 || notes != 0 {
		t.Fatalf("verdicts %+v notes %d", vs, notes)
	}
	v := vs[0]
	if v.Monitor != contract.MonitorSecondGuessing || v.Mode != contract.ModeNote || v.Action != contract.ModeNote ||
		strings.Join(v.Evidence.Phrases, ",") != "on second thought,let me reconsider,no wait" || v.Evidence.Count != 3 {
		t.Errorf("verdict %+v", v)
	}
	if est := v.Basis; est != (contract.EstBasis{}) {
		t.Errorf("a stream monitor saves nothing at fire time: %+v", est)
	}

	next := before(t, tollgate.WireOpenAIChat, fakeRing{}, sgAttrs(), 4, weatherTranscript(1))
	next.Session.ID = "s1"
	h.Before(bg, next)
	nvs, nnotes := verdictsOf(next)
	if nnotes != 1 || len(nvs) != 0 || countProxy(next.Body) != 1 || !strings.Contains(string(next.Body), "reversed itself") {
		t.Fatalf("the note rides the session's next call: notes %d verdicts %+v body %s", nnotes, nvs, next.Body)
	}
	after := before(t, tollgate.WireOpenAIChat, fakeRing{}, sgAttrs(), 5, weatherTranscript(1))
	after.Session.ID = "s1"
	h.Before(bg, after)
	if _, n := verdictsOf(after); n != 0 || countProxy(after.Body) != 0 {
		t.Error("a queued note is delivered once")
	}

	again := streamed(t, h, "s1", 3, doubtful)
	if vs, _ := verdictsOf(again); len(vs) != 1 || vs[0].Action != contract.ModeObserve {
		t.Errorf("the same finding in the same session is recorded but not noted again: %+v", vs)
	}
}

func TestStreamMonitorObserveQueuesNothingAndOtherSessionsAreApart(t *testing.T) {
	h := newMon(monitors.Options{})
	c := before(t, tollgate.WireOpenAIChat, sgRing(), nil, 3, []tollgate.Message{user("go")}) // defaults: observe
	c.Stream = true
	h.Before(bg, c)
	h.OnEvent(bg, c, tollgate.Event{Kind: tollgate.EventTextDelta, Text: doubtful})
	h.OnEvent(bg, c, readAgain())
	h.After(bg, c, okMetric())
	vs, _ := verdictsOf(c)
	if len(vs) != 1 || vs[0].Action != contract.ModeObserve {
		t.Fatalf("%+v", vs)
	}
	next := before(t, tollgate.WireOpenAIChat, fakeRing{}, nil, 4, weatherTranscript(1))
	h.Before(bg, next)
	if _, n := verdictsOf(next); n != 0 {
		t.Error("observe never queues a note")
	}
	streamed(t, h, "sA", 3, doubtful)
	other := before(t, tollgate.WireOpenAIChat, fakeRing{}, sgAttrs(), 4, weatherTranscript(1))
	other.Session.ID = "sB"
	h.Before(bg, other)
	if _, n := verdictsOf(other); n != 0 {
		t.Error("a note queued for one session must not reach another")
	}
}

func TestStreamDetectorsSkipFailedAndBufferedCalls(t *testing.T) {
	for name, mod := range map[string]func(*tollgate.Call, *tollgate.Metric){
		"stream:false":   func(c *tollgate.Call, _ *tollgate.Metric) { c.Stream = false },
		"upstream error": func(_ *tollgate.Call, m *tollgate.Metric) { m.Finish, m.Status = tollgate.FinishUpstreamError, 500 },
	} {
		t.Run(name, func(t *testing.T) {
			h := newMon(monitors.Options{})
			c := before(t, tollgate.WireOpenAIChat, sgRing(), sgAttrs(), 3, []tollgate.Message{user("go")})
			c.Stream = true
			h.Before(bg, c)
			h.OnEvent(bg, c, tollgate.Event{Kind: tollgate.EventTextDelta, Text: doubtful})
			h.OnEvent(bg, c, readAgain())
			m := okMetric()
			mod(c, &m)
			h.After(bg, c, m)
			if vs, _ := verdictsOf(c); len(vs) != 0 {
				t.Errorf("verdicts %+v", vs)
			}
		})
	}
}

func TestNoteThatTheChainDidNotAdoptIsCounted(t *testing.T) {
	h := newMon(monitors.Options{})
	c := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
	orig := append([]byte(nil), c.Body...)
	h.Before(bg, c)
	h.After(bg, c, okMetric())
	if h.Status()["notes_not_adopted"] != int64(0) {
		t.Fatal("the note is in the body: adopted")
	}
	c.Body = orig // the chain kept the old body
	h.After(bg, c, okMetric())
	if h.Status()["notes_not_adopted"] != int64(1) {
		t.Errorf("status %v", h.Status())
	}
}

func TestSessionStateIsBoundedAndDroppedOnClose(t *testing.T) {
	h := newMon(monitors.Options{MaxSessions: 16})
	for i := 0; i < 200; i++ {
		c := before(t, tollgate.WireOpenAIChat, dupRing(), dupMode(contract.ModeNote), 4, weatherTranscript(3))
		c.Session.ID = fmt.Sprint(i)
		h.Before(bg, c)
	}
	st := h.Status()
	if st["sessions"].(int) > 16 || st["sessions_evicted"].(int64) == 0 {
		t.Errorf("per-session state must stay bounded: %v", st)
	}
	h.OnSessionClosed(bg, tollgate.Session{ID: "199", TenantID: "ws"})
	h.OnSessionClosed(bg, tollgate.Session{ID: "nope", TenantID: "ws"})
}
