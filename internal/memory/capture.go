package memory

import (
	"context"
	"math/bits"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// Capture rule: a completed session of at least minCalls calls that changed a file is distilled into a trace; any
// other closed session leaves only a light failure record.
const (
	minCalls       = 4
	baselineWindow = 30 * 24 * time.Hour
	minBaseline    = 3 // completed un-hinted sessions needed before a likely credit is claimed
)

// OnClosed is told by the ledger of every session it really closed (ledger.Options.OnClosed). It takes the session's
// state and queues what the close calls for; it does no I/O itself.
func (e *Engine) OnClosed(ctx context.Context, ws, id, outcome string, calls int) {
	if outcome == contract.OutcomeCompleted {
		e.jobs.enqueue(ctx, ws, kindCredit, func(ctx context.Context) error { return e.credit(ctx, ws, id, calls) })
	}
	st := e.sess.take(ws, id)
	if st == nil {
		e.noState.Add(1)
		return
	}
	if !e.Keyed() || !st.capture || st.cfg.Mode == contract.MemoryModeOff {
		return
	}
	st.sum.Calls = st.calls
	sum := st.sum
	if outcome == contract.OutcomeCompleted && calls >= minCalls && st.wrote {
		cfg := st.cfg
		e.enqueueCaptured(ctx, ws, kindDistil, func(ctx context.Context) error { return e.distil(ctx, ws, id, cfg, sum) })
		return
	}
	lastErr := ""
	if n := len(sum.Errors); n > 0 {
		lastErr = sum.Errors[n-1]
	}
	f := failureOf(ws, id, calls, sum, lastErr)
	e.enqueueCaptured(ctx, ws, kindFailure, func(ctx context.Context) error { return e.opt.Store.InsertTraceFailure(ctx, f) })
}

// enqueueCaptured queues fn for the workspace, run only while the capture rule still holds when the job runs.
func (e *Engine) enqueueCaptured(ctx context.Context, ws string, k jobKind, fn func(ctx context.Context) error) {
	e.jobs.enqueue(ctx, ws, k, func(ctx context.Context) error {
		if !e.stillCaptured(ctx, ws) {
			return nil
		}
		return fn(ctx)
	})
}

// stillCaptured re-checks the capture rule when a session closes: a session summarized while capture was on is not
// stored if privacy has been switched on since (the strictest setting wins at the moment of writing). A failed read
// counts as not allowed.
func (e *Engine) stillCaptured(ctx context.Context, ws string) bool {
	if e.opt.PrivacyMode == nil || *e.opt.PrivacyMode {
		e.skippedPrivacy.Add(1)
		return false
	}
	ok, err := e.opt.Store.WorkspaceCaptureAllowed(ctx, ws)
	if err != nil || !ok {
		e.skippedPrivacy.Add(1)
		return false
	}
	return true
}

func summarySignature(s Summary) contract.TraceSignature {
	return contract.TraceSignature{Files: s.Files, Errors: s.Errors, Tools: s.Tools}
}

// failureOf is the light record of a session that left no trace.
func failureOf(ws, id string, calls int, sum Summary, lastErr string) store.TraceFailure {
	return store.TraceFailure{WorkspaceID: ws, SessionID: id, Calls: calls, Signature: summarySignature(sum), LastError: lastErr}
}

// distil turns a finished session into a stored trace: one model call, one embedding, one insert. A budget at its
// limit, a model failure or a bad answer stores a failure record instead, never a half trace.
func (e *Engine) distil(ctx context.Context, ws, id string, cfg contract.MemoryConfig, sum Summary) error {
	if e.overBudget(ctx, ws) {
		e.skippedBudget.Add(1)
		return nil
	}
	dm, em := e.distilModel(cfg), e.embedModel(cfg)
	tr, u, err := e.opt.Distiller.Distil(ctx, dm, sum)
	e.recordUsage(ctx, ws, "distil", dm, u)
	if err != nil {
		e.failure(ctx, ws, id, sum, "distil failed")
		return err
	}
	tr = tr.clean()
	if len(tr.Signature.Files) == 0 {
		tr.Signature.Files = sum.Files
	}
	if len(tr.Signature.Tools) == 0 {
		tr.Signature.Tools = sum.Tools
	}
	vec, eu, err := e.opt.Embedder.Embed(ctx, em, embedText(Summary{Task: sum.Task, Errors: sum.Errors}))
	e.recordUsage(ctx, ws, "embed", em, eu)
	if err != nil {
		e.failure(ctx, ws, id, sum, "embed failed")
		return err
	}
	_, _, err = e.opt.Store.InsertTrace(ctx, store.NewTrace{
		WorkspaceID: ws, SessionID: id, TaskSummary: tr.TaskSummary, Observations: tr.Observations, Approach: tr.Approach, Fix: tr.Fix,
		Signature: tr.Signature, Confidence: tr.Confidence, PromptV: PromptVersion, EmbedModel: em, Embedding: vec,
	})
	return err
}

func (e *Engine) failure(ctx context.Context, ws, id string, sum Summary, why string) {
	_ = e.opt.Store.InsertTraceFailure(ctx, failureOf(ws, id, sum.Calls, sum, why))
}

// credit sets the likely credit of a completed session's injected hit: how much fewer calls the session took than the
// workspace's usual completed session without a hint, priced at that baseline's average call. It is a likely number
// (never added to a certain one), claimed once, at most one average session in size, and only with a baseline of
// minBaseline sessions. The first injected hit carries it.
func (e *Engine) credit(ctx context.Context, ws, id string, calls int) error {
	hits, err := e.opt.Store.UncreditedHits(ctx, ws, id)
	if err != nil || len(hits) == 0 {
		return err
	}
	b, err := e.opt.Store.SessionBaseline(ctx, ws, id, e.opt.Now().Add(-baselineWindow))
	if err != nil || b.Sessions < minBaseline || b.Calls <= 0 {
		return err
	}
	fewer := int64((b.MedianCalls - float64(calls)) * 1000) // thousandths of a call
	if fewer <= 0 {
		return nil
	}
	usd := min(mulDiv(int64(b.Cost), fewer, b.Calls*1000), int64(b.Cost)/b.Sessions)
	tokens := min(mulDiv(b.Tokens, fewer, b.Calls*1000), b.Tokens/b.Sessions)
	return e.opt.Ledger.CreditMemory(ctx, ws, id, hits[0], tokens, contract.MicroUSD(usd))
}

// mulDiv is a*b/c without overflow in the product (all values non-negative, c > 0).
func mulDiv(a, b, c int64) int64 {
	if a <= 0 || b <= 0 || c <= 0 {
		return 0
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(c) {
		return 1<<63 - 1
	}
	q, _ := bits.Div64(hi, lo, uint64(c))
	return int64(min(q, 1<<63-1))
}
