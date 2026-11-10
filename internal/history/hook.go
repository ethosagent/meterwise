package history

import (
	"context"
	"sync/atomic"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

const metaLive = "history.live"

// Options tunes the ring store. The zero value is usable.
type Options struct {
	// MaxSessions bounds the live rings (cmd passes tollgate's Limits.MaxSessions); beyond it the
	// least recently used session is dropped and counted. Default 100000.
	MaxSessions int
	// Shards is the number of lock shards. Default 32.
	Shards int
}

// Hook is the history tollgate Hook (Name "history"). It does no I/O and no clock reads.
type Hook struct {
	tollgate.NoopHook
	st       *store
	evicted  atomic.Int64
	caughtUp atomic.Int64
	rebuilt  atomic.Int64
	appended atomic.Int64
	healed   atomic.Int64
}

// New builds a Hook.
func New(o Options) *Hook {
	if o.MaxSessions <= 0 {
		o.MaxSessions = 100000
	}
	if o.Shards <= 0 {
		o.Shards = 32
	}
	h := &Hook{}
	h.st = newStore(o.MaxSessions, o.Shards, func() { h.evicted.Add(1) })
	return h
}

// Name identifies the hook.
func (*Hook) Name() string { return "history" }

func key(tenant, session string) string { return tenant + "\x00" + session }

// Before stamps Meta with the ring snapshot and a fresh verdict collection. It never short-circuits,
// so its Call copy is adopted (ruling R3). An empty ring is rebuilt from the call's messages, and a ring
// that trails them (the previous call's After has not run yet) is caught up from the same messages.
func (h *Hook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Provider.Wire == tollgate.WirePassthrough || c.Session.ID == "" {
		return nil, nil
	}
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	k := key(c.Tenant.ID, c.Session.ID)
	calls := h.st.get(k)
	switch {
	case len(calls) == 0 && len(c.Messages) > 0:
		calls = rebuild(c)
		if len(calls) > 0 {
			h.rebuilt.Add(1)
		}
		h.st.update(k, func([]contract.CallSummary) []contract.CallSummary { return calls })
	case len(calls) > 0 && trails(calls, c):
		calls = h.catchUp(k, c)
	case len(calls) > 0 && emptyTurn(calls[len(calls)-1]):
		calls = h.heal(k, c, calls)
	}
	c.Meta[contract.MetaRing] = ringView(calls)
	c.Meta[contract.MetaMonitors] = &contract.CallVerdicts{}
	c.Meta[metaLive] = &live{index: c.Session.Calls}
	return nil, nil
}

// trails reports whether the request carries more assistant turns than the ring holds (capped at the
// ring depth and the rebuild window): the newest turns are in the messages but not yet in the ring.
func trails(calls []contract.CallSummary, c *tollgate.Call) bool {
	return len(calls) < min(assistantTurns(c), RingDepth)
}

// catchUp appends the missing newest turns, read from the call's messages, to the session's ring. After
// runs once the response is out, so a client faster than it sends its next request first; the late After
// then fills the entry that has its index instead of adding another.
func (h *Hook) catchUp(k string, c *tollgate.Call) []contract.CallSummary {
	var out []contract.CallSummary
	h.st.update(k, func(cur []contract.CallSummary) []contract.CallSummary {
		out = cur
		if !trails(cur, c) {
			return cur
		}
		out = extend(cur, c)
		h.caughtUp.Add(1)
		return out
	})
	return out
}

func emptyTurn(s contract.CallSummary) bool { return len(s.Tools) == 0 && s.TextHash == 0 }

// heal replaces a newest entry that has no content (a call answered without a stream, such as a
// block) with what the client's last assistant message says that turn was.
func (h *Hook) heal(k string, c *tollgate.Call, calls []contract.CallSummary) []contract.CallSummary {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role != tollgate.RoleAssistant {
			continue
		}
		fresh := fromMessage(c.Messages[i]).summary()
		old := calls[len(calls)-1]
		fresh.Index, fresh.Input, fresh.Output, fresh.BodyBytes = old.Index, old.Input, old.Output, old.BodyBytes
		out := append(append([]contract.CallSummary(nil), calls[:len(calls)-1]...), fresh)
		h.st.update(k, func([]contract.CallSummary) []contract.CallSummary { return out })
		h.healed.Add(1)
		return out
	}
	return calls
}

// OnEvent collects the stream's text, reasoning and tool calls, off the client path.
func (*Hook) OnEvent(_ context.Context, c *tollgate.Call, e tollgate.Event) {
	l, _ := c.Meta[metaLive].(*live)
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	switch e.Kind {
	case tollgate.EventTextDelta:
		l.turn.addText(e.Text)
	case tollgate.EventReasoningDelta:
		l.turn.addReasoning(e.Text)
	case tollgate.EventToolCallDelta:
		if e.ToolCall != nil {
			l.tool(e.ToolCall)
		}
	}
}

// After appends the finished call to its session's ring. A call that failed upstream adds nothing.
func (h *Hook) After(_ context.Context, c *tollgate.Call, m tollgate.Metric) {
	l, _ := c.Meta[metaLive].(*live)
	if l == nil || m.Finish != tollgate.FinishOK || m.Status >= 400 {
		return
	}
	l.mu.Lock()
	s := l.finish().summary()
	l.mu.Unlock()
	s.Index = l.index
	s.Input = m.Usage.InputTokens + m.Usage.CachedInputTokens
	s.Output = m.Usage.OutputTokens
	s.BodyBytes = len(c.Body)
	h.st.update(key(c.Tenant.ID, c.Session.ID), func(cur []contract.CallSummary) []contract.CallSummary {
		return appendCall(cur, s)
	})
	h.appended.Add(1)
}

// OnSessionClosed drops the session's ring.
func (h *Hook) OnSessionClosed(_ context.Context, s tollgate.Session) {
	h.st.remove(key(s.TenantID, s.ID))
}

// Status is the hook's counters for /statusz.
func (h *Hook) Status() map[string]any {
	return map[string]any{
		"sessions": h.st.size(), "evicted": h.evicted.Load(), "rebuilt": h.rebuilt.Load(),
		"appended": h.appended.Load(), "healed": h.healed.Load(), "caught_up": h.caughtUp.Load(),
	}
}
