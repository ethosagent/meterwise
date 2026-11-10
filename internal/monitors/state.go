package monitors

import (
	"slices"
	"sync"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

const metaState = "monitors.state"

// Bounds on what one call collects for the stream detectors.
const (
	maxStreamBytes  = 32 << 10
	maxStreamTokens = 4200
	maxWindows      = 4096
)

// callState is what the Hook keeps for one call: the resolved config, how many notes went into the
// request (After checks they were adopted) and, for streams, the answer text, the reasoning and the tool
// calls OnEvent saw, each bounded. It lives in Meta as a pointer, so a late write cannot corrupt a copy.
type callState struct {
	cfg   contract.MonitorConfig
	key   string // the session's note-state key, computed once per call
	noted int

	mu     sync.Mutex
	buf    []byte // answer text
	reason []byte // reasoning text
	tools  map[int]*streamTool
	args   int         // argument bytes kept, across tools
	snap   *streamSnap // the snapshot of what is collected so far; nil after any event
}

// streamSnap is what the stream detectors read: the answer text, the reasoning, and the named tool calls in
// call order. After takes it once for both detectors.
type streamSnap struct {
	text, reasoning string
	tools           []streamTool
}

// streamTool is one tool call of the response, reassembled by index.
type streamTool struct {
	name string
	args []byte
}

func stateOf(c *tollgate.Call) *callState {
	st, _ := c.Meta[metaState].(*callState)
	return st
}

func appendCapped(dst []byte, s string) []byte {
	if room := maxStreamBytes - len(dst); room > 0 {
		if len(s) > room {
			s = s[:room]
		}
		dst = append(dst, s...)
		dst = append(dst, ' ')
	}
	return dst
}

// event records one stream event; it is the only writer after Before.
func (s *callState) event(e tollgate.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snap = nil
	switch e.Kind {
	case tollgate.EventTextDelta:
		s.buf = appendCapped(s.buf, e.Text)
	case tollgate.EventReasoningDelta:
		s.reason = appendCapped(s.reason, e.Text)
	case tollgate.EventToolCallDelta:
		if e.ToolCall == nil {
			return
		}
		if s.tools == nil {
			s.tools = map[int]*streamTool{}
		}
		t := s.tools[e.ToolCall.Index]
		if t == nil {
			t = &streamTool{}
			s.tools[e.ToolCall.Index] = t
		}
		if e.ToolCall.Name != "" {
			t.name = e.ToolCall.Name
		}
		if s.args+len(e.ToolCall.Arguments) <= maxStreamBytes {
			t.args = append(t.args, e.ToolCall.Arguments...)
			s.args += len(e.ToolCall.Arguments)
		}
	}
}

// snapshot returns the collected answer text, the reasoning, and the named tool calls in call order. The
// result is built once and shared until the next event; callers must not modify it.
func (s *callState) snapshot() *streamSnap {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snap != nil {
		return s.snap
	}
	idx := make([]int, 0, len(s.tools))
	for i := range s.tools {
		idx = append(idx, i)
	}
	slices.Sort(idx)
	snap := &streamSnap{text: string(s.buf), reasoning: string(s.reason)}
	for _, i := range idx {
		if t := s.tools[i]; t.name != "" {
			snap.tools = append(snap.tools, streamTool{name: t.name, args: slices.Clone(t.args)})
		}
	}
	s.snap = snap
	return snap
}

// empty reports whether the call collected nothing a stream detector could read.
func (s *callState) empty() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf) == 0 && len(s.reason) == 0 && len(s.tools) == 0
}
