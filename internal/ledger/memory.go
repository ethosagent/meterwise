package ledger

import (
	"context"
	"errors"
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// ClosedSession is a session a close statement really closed: the outcome the heuristic gave it and its call count.
type ClosedSession struct {
	WorkspaceID string
	SessionID   string
	Outcome     string // contract.OutcomeCompleted or contract.OutcomeAbandoned
	Calls       int
}

// SetOnClosed sets the callback told of every session this ledger closes (see Options.OnClosed); nil clears it.
func (h *Hook) SetOnClosed(fn func(ClosedSession)) {
	if fn == nil {
		h.q.onClosed.Store(nil)
		return
	}
	h.q.onClosed.Store(&fn)
}

// closeReturner is the optional Writer extension that names the sessions a close statement really closed.
type closeReturner interface {
	CloseSessionsReturning(ctx context.Context, closes []contract.SessionClose) ([]store.ClosedSession, error)
}

// hitWriter is the optional Writer extension that writes a batch's memory hits in the calls' transaction.
type hitWriter interface {
	WriteCallsWithHits(ctx context.Context, recs []contract.CallRecord, hits []store.MemoryHit) (store.WriteResult, error)
}

// writeOnce writes recs and, when the Writer can, the hits of those calls in the same transaction.
func (q *queue) writeOnce(ctx context.Context, recs []contract.CallRecord, hits []store.MemoryHit) (store.WriteResult, error) {
	hw, ok := q.o.Writer.(hitWriter)
	if !ok || len(hits) == 0 {
		return q.o.Writer.WriteCalls(ctx, recs)
	}
	mine := make([]store.MemoryHit, 0, len(hits))
	for _, h := range hits {
		if slices.ContainsFunc(recs, func(r contract.CallRecord) bool { return r.ID == h.CallID }) {
			mine = append(mine, h)
		}
	}
	return hw.WriteCallsWithHits(ctx, recs, mine)
}

// notifyClosed tells the OnClosed callback of each session a close really closed. A panic in the callback is
// contained: the batcher must outlive a faulty consumer.
func (q *queue) notifyClosed(_ context.Context, closed []store.ClosedSession) {
	fn := q.onClosed.Load()
	if fn == nil {
		return
	}
	for _, c := range closed {
		q.callClosed(*fn, ClosedSession{WorkspaceID: c.WorkspaceID, SessionID: c.SessionID, Outcome: c.Outcome, Calls: c.Calls})
	}
}

func (q *queue) callClosed(fn func(ClosedSession), s ClosedSession) {
	defer func() {
		if recover() != nil {
			q.closedPanics.Add(1)
		}
	}()
	fn(s)
}

// memoryHitOf is the call's memory hit, from Meta[memory]: a call that reached the upstream and was not blocked, whose
// hook reported a trace. A value of the wrong shape records nothing (savers.go has already counted it in
// invalid_saver_meta); so does a hint that names no trace or no mode.
func (h *Hook) memoryHitOf(c *tollgate.Call, m tollgate.Metric, rec *contract.CallRecord) *store.MemoryHit {
	raw := c.Meta[contract.MetaMemory]
	if raw == nil || rec.Blocked || noUsageOf(m) {
		return nil
	}
	mh, ok := hintOf(raw)
	if !ok || mh.TraceID <= 0 || (mh.Mode != contract.MemoryModeObserve && mh.Mode != contract.MemoryModeHint) {
		return nil
	}
	return &store.MemoryHit{CallID: rec.ID, TraceID: mh.TraceID, SimilarityBp: mh.SimilarityBp, Mode: mh.Mode, Injected: mh.Injected}
}

func noUsageOf(m tollgate.Metric) bool {
	return m.Usage.InputTokens <= 0 && m.Usage.OutputTokens <= 0 && m.Usage.CachedInputTokens <= 0 && m.Usage.ReasoningTokens <= 0
}

// MemoryStore is what the memory port needs from the store; *store.Store implements it and tests fake it.
type MemoryStore interface {
	SetMemoryHitFeedback(ctx context.Context, workspaceID string, hitID int64, fb *string) (store.HitRef, error)
	CreditMemoryHit(ctx context.Context, workspaceID, sessionID string, hitID, tokens int64, usd contract.MicroUSD) (bool, error)
}

// MemoryPort is how the memory feature reaches the memory_hits writes. memory_hits is a ledger table (H4: only the
// ledger writes it), so hit feedback and the likely credit travel through this type, not through the memory package.
type MemoryPort struct{ st MemoryStore }

// NewMemoryPort wraps the store writes.
func NewMemoryPort(st MemoryStore) *MemoryPort { return &MemoryPort{st: st} }

// SetHitFeedback sets ("helpful" or "irrelevant") or clears (nil) the feedback of one hit of the workspace and says
// which trace it belongs to. Another value is a 422 on "feedback"; a hit outside the workspace is not_found.
func (p *MemoryPort) SetHitFeedback(ctx context.Context, workspaceID string, hitID int64, fb *string) (store.HitRef, error) {
	if !contract.ValidMemoryFeedback(fb) {
		return store.HitRef{}, contract.Invalid("feedback", `must be "helpful", "irrelevant" or null`)
	}
	return p.st.SetMemoryHitFeedback(ctx, workspaceID, hitID, fb)
}

// CreditMemory sets the likely credit of one injected hit of a closed session, once, and adds it to the session's
// likely number. It never touches the certain number. A negative amount is refused.
func (p *MemoryPort) CreditMemory(ctx context.Context, workspaceID, sessionID string, hitID, tokens int64, usd contract.MicroUSD) error {
	if tokens < 0 || usd < 0 {
		return errors.New("ledger: a memory credit cannot be negative")
	}
	_, err := p.st.CreditMemoryHit(ctx, workspaceID, sessionID, hitID, tokens, usd)
	return err
}
