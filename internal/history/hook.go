package history

import (
	"context"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

const metaLive = "history.live"

// Options tunes the ring store. The zero value is usable.
type Options struct {
	// MaxSessions bounds the live rings (cmd passes tollgate's Limits.MaxSessions); beyond it the
	// least recently used session is dropped and counted. Default 100000.
	MaxSessions int
	// MaxBytes bounds the bytes the rings hold (an estimate per entry); beyond it the least recently used
	// sessions are dropped and counted. cmd passes HISTORY_MAX_BYTES. Default 256 MiB.
	MaxBytes int64
	// Shards is the number of lock shards. Default 32.
	Shards int
	// Now reads the clock for the deadline check only. Default time.Now.
	Now func() time.Time
	// MinDeadline: when the chain's deadline is nearer than this, Before reads no more turns from the
	// request (it still stamps Meta with the ring it has). Default 5ms, the monitors' own margin.
	MinDeadline time.Duration
}

// Hook is the history tollgate Hook (Name "history"). It does no I/O, and reads the clock only for the
// deadline check.
type Hook struct {
	tollgate.NoopHook
	opt      Options
	st       *store
	skipped  atomic.Int64
	cut      atomic.Int64
	evicted  atomic.Int64
	evictedB atomic.Int64
	caughtUp atomic.Int64
	rebuilt  atomic.Int64
	appended atomic.Int64
	healed   atomic.Int64
	unhealed atomic.Int64
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
	if o.MaxBytes <= 0 {
		o.MaxBytes = DefaultMaxBytes
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MinDeadline <= 0 {
		o.MinDeadline = 5 * time.Millisecond
	}
	h := &Hook{opt: o}
	h.st = newStore(o.MaxSessions, o.MaxBytes, o.Shards, func(byBytes bool) {
		if byBytes {
			h.evictedB.Add(1)
			return
		}
		h.evicted.Add(1)
	})
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
// block) is healed from the request's newest turn. Both only when the request's older turns match the
// ring's (aligned): a side request that shares the session id fills nothing (K3).
func (h *Hook) Before(ctx context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Provider.Wire == tollgate.WirePassthrough || c.Session.ID == "" {
		return nil, nil
	}
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	k := key(c.Tenant.ID, c.Session.ID)
	cur, ok := h.st.get(k)
	fills := h.missing(ctx, cur, ok, c)
	if !ok || len(fills) > 0 {
		cur = h.st.update(k, func(r ring) ring { return r.with(fills, true) })
	}
	c.Meta[contract.MetaRing] = ringView(cur.calls)
	c.Meta[contract.MetaMonitors] = &contract.CallVerdicts{}
	c.Meta[metaLive] = &live{index: c.Session.Calls}
	return nil, nil
}

// missing reads, from the call's messages, the ring entries the session lacks: all of them when it has no
// ring, else the indexes above the newest it knows (at most RingDepth back) and the previous call's entry
// when it is empty.
func (h *Hook) missing(ctx context.Context, cur ring, ok bool, c *tollgate.Call) []contract.CallSummary {
	n, msgs := c.Session.Calls, window(c.Messages)
	all := func(int) bool { return true }
	if !ok {
		fills := readBack(msgs, n, n-RingDepth, n-1, all, h.late(ctx))
		h.read.Add(int64(len(fills)))
		if len(fills) > 0 {
			h.rebuilt.Add(1)
		}
		return fills
	}
	lo := max(cur.newest()+1, n-RingDepth)
	heal := emptyAt(cur.calls, n-1)
	catch := lo <= n-1
	if !catch && !heal {
		return nil // the common case: the ring is current, nothing is read and no deadline is asked
	}
	late := h.late(ctx)
	if !aligned(cur.calls, msgs, n, late) {
		h.unhealed.Add(1) // a side request's turns are neither a catch-up nor a heal (K3, S1)
		return nil
	}
	from := lo
	if heal {
		from = min(lo, n-1)
	}
	fills := readBack(msgs, n, from, n-1, func(i int) bool { return i >= lo || (heal && i == n-1) }, late)
	h.read.Add(int64(len(fills)))
	switch {
	case len(fills) == 0:
	case catch:
		h.caughtUp.Add(1)
	default:
		h.healed.Add(1)
	}
	return fills
}

// late returns the deadline check readBack asks before each turn it parses: true once the chain's
// deadline is nearer than MinDeadline. The first time it is true is counted (skipped_deadline when the
// read had not started, cut_deadline when it stops a read part way).
func (h *Hook) late(ctx context.Context) func() bool {
	if _, ok := ctx.Deadline(); !ok {
		return func() bool { return false }
	}
	asked := false
	return func() bool {
		if !contract.NearDeadline(ctx, h.opt.Now(), h.opt.MinDeadline) {
			asked = true
			return false
		}
		if asked {
			h.cut.Add(1)
		} else {
			h.skipped.Add(1)
		}
		return true
	}
}

// aligned reports whether the request's turns line up with the ring before a catch-up or a heal of entry n-1: each of
// the ring's entries n-2 .. n-1-alignDepth that has content must be the request's turn at that index. A
// side request that shares the session id (title generation, another conversation) leaves an empty entry
// n-1 while the main thread's next request carries, as its newest turn, the turn the ring already holds
// at n-2; healing would copy it into n-1 and make a false duplicate (S1). Checking a few entries keeps a
// true repeat (the same turn at n-3 and n-2) from passing by chance. With no content to check against,
// the heal goes ahead.
func aligned(calls []contract.CallSummary, msgs []tollgate.Message, n int, late func() bool) bool {
	lo, hi := n-1-alignDepth, n-2
	var want [alignDepth]*contract.CallSummary // want[hi-idx]: the ring's entry at idx, when it has content
	wanted := 0
	for i := range calls {
		if s := &calls[i]; s.Index >= lo && s.Index <= hi && !emptyTurn(*s) {
			want[hi-s.Index] = s
			wanted++
		}
	}
	if wanted == 0 {
		return true
	}
	k, matched := 0, 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != tollgate.RoleAssistant {
			continue
		}
		k++
		idx := n - k
		if idx < lo {
			break
		}
		if idx > hi || want[hi-idx] == nil {
			continue
		}
		if late() { // the deadline ends the check: what cannot be compared is not aligned
			return false
		}
		if !sameTurn(*want[hi-idx], msgs[i]) {
			return false
		}
		matched++
	}
	return matched == wanted
}

// alignDepth is how many ring entries below a heal are checked against the request.
const alignDepth = 3

// sameTurn compares a ring entry with an assistant message: the tools, their arguments and the text.
func sameTurn(s contract.CallSummary, m tollgate.Message) bool {
	if s.TextHash != contract.NormalizedHash(m.Text) || len(s.Tools) != len(m.ToolCalls) || len(s.ArgsHashes) != len(m.ToolCalls) {
		return false
	}
	for i, tc := range m.ToolCalls {
		if s.Tools[i] != tc.Name || s.ArgsHashes[i] != contract.ToolArgsHash(tc.Name, tc.Arguments) {
			return false
		}
	}
	return true
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
	s := l.finish().summary(true)
	l.mu.Unlock()
	s.Index = l.index
	s.Input = m.Usage.InputTokens + m.Usage.CachedInputTokens
	s.Output = m.Usage.OutputTokens
	s.BodyBytes, s.UsageKnown = len(c.Body), true
	h.st.update(k, func(r ring) ring {
		r.top = max(r.top, l.index)
		return r.with([]contract.CallSummary{s}, false)
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
		"sessions": h.st.size(), "bytes": h.st.held(), "evicted": h.evicted.Load(), "evicted_bytes": h.evictedB.Load(), "rebuilt": h.rebuilt.Load(),
		"appended": h.appended.Load(), "healed": h.healed.Load(), "heal_misaligned": h.unhealed.Load(), "caught_up": h.caughtUp.Load(), "turns_read": h.read.Load(),
		"skipped_deadline": h.skipped.Load(), "cut_deadline": h.cut.Load(),
	}
}
