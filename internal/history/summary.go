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

// window is what a ring is read from: the request's newest maxRebuildMessages messages, without a
// trailing assistant message, which is a prefill (the client starts the answer it asks for), not a turn.
func window(msgs []tollgate.Message) []tollgate.Message {
	if n := len(msgs); n > 0 && msgs[n-1].Role == tollgate.RoleAssistant {
		msgs = msgs[:n-1]
	}
	if len(msgs) > maxRebuildMessages {
		msgs = msgs[len(msgs)-maxRebuildMessages:]
	}
	return msgs
}

// readBack reduces the assistant turns of msgs, newest first, to ring entries: the k-th newest is call
// index calls-k (the request carries every earlier call's turn, the newest last). Only indexes in
// [lo, hi] that want accepts are parsed, and the walk stops below lo, so a catch-up parses only the
// turns it misses. stop is asked before each parse; true ends the walk (the deadline). Token counts of
// the entries are unknown (0).
func readBack(msgs []tollgate.Message, calls, lo, hi int, want func(int) bool, stop func() bool) []contract.CallSummary {
	var out []contract.CallSummary
	k := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != tollgate.RoleAssistant {
			continue
		}
		k++
		idx := calls - k
		if idx < lo {
			break
		}
		if idx > hi || !want(idx) {
			continue
		}
		if stop() {
			break
		}
		s := fromMessage(msgs[i]).summary()
		s.Index = idx
		out = append(out, s)
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
