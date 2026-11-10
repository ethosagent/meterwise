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
// request (After checks they were adopted) and, for streams, the text and tool names OnEvent saw.
// It lives in Meta as a pointer, so a late write cannot corrupt a copy.
type callState struct {
	cfg   contract.MonitorConfig
	noted int

	mu    sync.Mutex
	buf   []byte
	tools map[int]string
}

func stateOf(c *tollgate.Call) *callState {
	st, _ := c.Meta[metaState].(*callState)
	return st
}

// event records one stream event; it is the only writer after Before.
func (s *callState) event(e tollgate.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e.Kind {
	case tollgate.EventTextDelta, tollgate.EventReasoningDelta:
		if room := maxStreamBytes - len(s.buf); room > 0 {
			t := e.Text
			if len(t) > room {
				t = t[:room]
			}
			s.buf = append(s.buf, t...)
			s.buf = append(s.buf, ' ')
		}
	case tollgate.EventToolCallDelta:
		if e.ToolCall != nil && e.ToolCall.Name != "" {
			if s.tools == nil {
				s.tools = map[int]string{}
			}
			s.tools[e.ToolCall.Index] = e.ToolCall.Name
		}
	}
}

// snapshot returns the collected text and the tool names in call order.
func (s *callState) snapshot() (text string, tools []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := make([]int, 0, len(s.tools))
	for i := range s.tools {
		idx = append(idx, i)
	}
	slices.Sort(idx)
	for _, i := range idx {
		tools = append(tools, s.tools[i])
	}
	return string(s.buf), tools
}
