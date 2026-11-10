package history

import (
	"context"
	"slices"
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
	read     atomic.Int64
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
// so its Call copy is adopted (ruling R3). The ring is keyed by call index. A session with no ring is
// rebuilt from the call's messages (a restart, or an evicted ring). A ring behind the call (the previous
// call's After has not run yet: a client faster than After) is caught up from the same messages, and only
// the missing turns are parsed. An empty newest entry (a call answered without a stream, such as a
// block) is healed from the request's newest turn.
func (h *Hook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Provider.Wire == tollgate.WirePassthrough || c.Session.ID == "" {
		return nil, nil
	}
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	k := key(c.Tenant.ID, c.Session.ID)
	cur, ok := h.st.get(k)
	fills := h.missing(cur, ok, c)
	if !ok || len(fills) > 0 {
		cur = h.st.update(k, func(r ring) ring {
			r.calls = put(r.calls, fills, true)
			return r
		})
	}
	c.Meta[contract.MetaRing] = ringView(cur.calls)
	c.Meta[contract.MetaMonitors] = &contract.CallVerdicts{}
	c.Meta[metaLive] = &live{index: c.Session.Calls}
	return nil, nil
}

// missing reads, from the call's messages, the ring entries the session lacks: all of them when it has no
// ring, else the indexes above the newest it knows (at most RingDepth back) and the previous call's entry
// when it is empty.
func (h *Hook) missing(cur ring, ok bool, c *tollgate.Call) []contract.CallSummary {
	n, msgs := c.Session.Calls, window(c.Messages)
	never := func() bool { return false }
	if !ok {
		fills := readBack(msgs, n, n-RingDepth, n-1, func(int) bool { return true }, never)
		h.read.Add(int64(len(fills)))
		if len(fills) > 0 {
			h.rebuilt.Add(1)
		}
		return fills
	}
	lo := max(cur.newest()+1, n-RingDepth)
	heal := emptyAt(cur.calls, n-1)
	if lo > n-1 && !heal {
		return nil
	}
	from := lo
	if heal {
		from = min(lo, n-1)
	}
	fills := readBack(msgs, n, from, n-1, func(i int) bool { return i >= lo || (heal && i == n-1) }, never)
	h.read.Add(int64(len(fills)))
	switch {
	case len(fills) == 0:
	case lo <= n-1:
		h.caughtUp.Add(1)
	default:
		h.healed.Add(1)
	}
	return fills
}

// emptyAt reports whether the ring holds an entry for call index i with no content.
func emptyAt(calls []contract.CallSummary, i int) bool {
	j, found := slices.BinarySearchFunc(calls, i, byIndex)
	return found && emptyTurn(calls[j])
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

// After puts the finished call into its session's ring at its call index. A call that failed upstream adds
// no entry, but still counts as known, so a later catch-up does not fill its index from another turn.
func (h *Hook) After(_ context.Context, c *tollgate.Call, m tollgate.Metric) {
	l, _ := c.Meta[metaLive].(*live)
	if l == nil {
		return
	}
	k := key(c.Tenant.ID, c.Session.ID)
	if m.Finish != tollgate.FinishOK || m.Status >= 400 {
		h.st.update(k, func(r ring) ring {
			r.top = max(r.top, l.index)
			return r
		})
		return
	}
	l.mu.Lock()
	s := l.finish().summary()
	l.mu.Unlock()
	s.Index = l.index
	s.Input = m.Usage.InputTokens + m.Usage.CachedInputTokens
	s.Output = m.Usage.OutputTokens
	s.BodyBytes = len(c.Body)
	h.st.update(k, func(r ring) ring {
		r.top = max(r.top, l.index)
		r.calls = put(r.calls, []contract.CallSummary{s}, false)
		return r
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
		"appended": h.appended.Load(), "healed": h.healed.Load(), "caught_up": h.caughtUp.Load(), "turns_read": h.read.Load(),
	}
}
