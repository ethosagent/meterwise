package history

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// turn is the content of one assistant turn: what both the live stream and a rebuilt message reduce to.
type turn struct {
	tools  []toolCall
	text   contract.TextHasher
	reason string // first ShingleTextCap bytes
	prefix string // first ShingleTextCap bytes of text
}

type toolCall struct {
	name string
	args []byte
}

func (t *turn) addText(s string) {
	t.text.Write(s)
	t.prefix = capAppend(t.prefix, s)
}

func (t *turn) addReasoning(s string) { t.reason = capAppend(t.reason, s) }

func capAppend(dst, s string) string {
	if room := contract.ShingleTextCap - len(dst); room > 0 {
		if len(s) > room {
			s = s[:room]
		}
		return dst + s
	}
	return dst
}

// summary reduces the turn to a CallSummary (Index, token counts and BodyBytes are the caller's).
func (t *turn) summary() contract.CallSummary {
	s := contract.CallSummary{TextHash: t.text.Sum()}
	for _, tc := range t.tools {
		s.Tools = append(s.Tools, tc.name)
		h := contract.ToolArgsHash(tc.name, tc.args)
		s.ArgsHashes = append(s.ArgsHashes, h)
		switch contract.ClassifyTool(tc.name) {
		case contract.ToolRead:
			s.ReadHashes = append(s.ReadHashes, contract.ToolSubject(tc.name, tc.args))
		case contract.ToolWrite:
			s.WroteHashes = append(s.WroteHashes, contract.ToolSubject(tc.name, tc.args))
		}
	}
	s.Shingles = contract.TurnShingles(t.reason, t.prefix)
	return s
}

// fromMessage reduces an assistant message to a turn.
func fromMessage(m tollgate.Message) *turn {
	t := &turn{}
	t.addReasoning(m.Reasoning)
	if m.Text != "" {
		t.addText(m.Text)
	}
	for _, tc := range m.ToolCalls {
		t.tools = append(t.tools, toolCall{name: tc.Name, args: tc.Arguments})
	}
	return t
}

// rebuild derives a session's ring from the request's own messages: one entry per assistant turn,
// newest last, indexed backwards from the current call. Token counts are unknown (0).
func rebuild(c *tollgate.Call) []contract.CallSummary {
	msgs := c.Messages
	if len(msgs) > maxRebuildMessages {
		msgs = msgs[len(msgs)-maxRebuildMessages:]
	}
	var out []contract.CallSummary
	for i := len(msgs) - 1; i >= 0 && len(out) < RingDepth; i-- {
		if msgs[i].Role == tollgate.RoleAssistant {
			out = append(out, fromMessage(msgs[i]).summary())
		}
	}
	slices.Reverse(out)
	for i := range out {
		out[i].Index = max(1, c.Session.Calls-len(out)+i)
		if i < len(out)-ringShingles {
			out[i].Shingles = nil
		}
	}
	return out
}

// assistantTurns counts the assistant messages in the rebuild window of c.
func assistantTurns(c *tollgate.Call) int {
	msgs := c.Messages
	if len(msgs) > maxRebuildMessages {
		msgs = msgs[len(msgs)-maxRebuildMessages:]
	}
	n := 0
	for i := range msgs {
		if msgs[i].Role == tollgate.RoleAssistant {
			n++
		}
	}
	return n
}

// extend returns cur plus the turns it misses, taken from the newest assistant messages of c and
// indexed backwards from the current call. Token counts of the new entries are unknown (0).
func extend(cur []contract.CallSummary, c *tollgate.Call) []contract.CallSummary {
	missing := min(assistantTurns(c), RingDepth) - len(cur)
	fresh := rebuild(c)
	if missing <= 0 || len(fresh) < missing {
		return cur
	}
	out := cur
	for _, s := range fresh[len(fresh)-missing:] {
		out = appendCall(out, s)
	}
	return out
}

// maxRebuildMessages bounds the messages a rebuild reads.
const maxRebuildMessages = 200

// live collects one streamed call (OnEvent) until After turns it into a ring entry.
type live struct {
	mu    sync.Mutex
	index int
	turn  turn
	calls map[int]*liveTool
	size  int
}

type liveTool struct {
	name string
	args strings.Builder
}

// maxArgBytes bounds the argument bytes kept per call.
const maxArgBytes = 64 << 10

func (l *live) tool(d *tollgate.ToolCallDelta) {
	if l.calls == nil {
		l.calls = map[int]*liveTool{}
	}
	lt := l.calls[d.Index]
	if lt == nil {
		lt = &liveTool{}
		l.calls[d.Index] = lt
	}
	if d.Name != "" {
		lt.name = d.Name
	}
	if l.size+len(d.Arguments) <= maxArgBytes {
		lt.args.WriteString(d.Arguments)
		l.size += len(d.Arguments)
	}
}

// finish orders the collected tool calls by index and returns the turn.
func (l *live) finish() *turn {
	idx := make([]int, 0, len(l.calls))
	for i := range l.calls {
		idx = append(idx, i)
	}
	slices.Sort(idx)
	for _, i := range idx {
		lt := l.calls[i]
		l.turn.tools = append(l.turn.tools, toolCall{name: lt.name, args: json.RawMessage(lt.args.String())})
	}
	return &l.turn
}
