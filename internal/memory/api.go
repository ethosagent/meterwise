package memory

import (
	"context"
	"strconv"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// API is the control-plane port (api.MemoryAPI). Every call is scoped by the workspace the gateway authorized.
type API struct{ e *Engine }

// API returns the control-plane port of the engine.
func (e *Engine) API() *API { return &API{e: e} }

// Status says whether memory works for the workspace and, when not, why: no model key, privacy (capture is off, so
// nothing may be read or stored), or the daily self budget.
func (a *API) Status(ctx context.Context, workspaceID string) (bool, string, error) {
	e := a.e
	if !e.Keyed() {
		return false, contract.MemoryDisabledNoKey, nil
	}
	if e.opt.PrivacyMode == nil || *e.opt.PrivacyMode {
		return false, contract.MemoryDisabledPrivacy, nil
	}
	ok, err := e.opt.Store.WorkspaceCaptureAllowed(ctx, workspaceID)
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, contract.MemoryDisabledPrivacy, nil
	}
	if e.overBudget(ctx, workspaceID) {
		return false, contract.MemoryDisabledBudget, nil
	}
	return true, "", nil
}

// Stats reads what memory did over the range; the handler fills Mode.
func (a *API) Stats(ctx context.Context, workspaceID string, r contract.SavingsRange) (contract.MemoryStats, error) {
	out, err := a.e.opt.Store.MemoryStats(ctx, workspaceID, r, a.e.opt.Now())
	if err != nil {
		return contract.MemoryStats{}, err
	}
	out.BudgetPaused = a.e.Keyed() && a.e.overBudget(ctx, workspaceID)
	return out, nil
}

// Traces lists the workspace's traces, newest first.
func (a *API) Traces(ctx context.Context, workspaceID, cursor string, limit int) (contract.TraceList, error) {
	return a.e.opt.Store.ListTraces(ctx, workspaceID, cursor, limit)
}

// Trace reads one trace of the workspace.
func (a *API) Trace(ctx context.Context, workspaceID, traceID string) (contract.TraceDetail, error) {
	return a.e.opt.Store.TraceDetail(ctx, workspaceID, traceID)
}

// Forget removes every trace of the workspace and, once that succeeded, drops the recalled candidates still waiting in
// sessions, so a forgotten trace is never injected afterwards.
func (a *API) Forget(ctx context.Context, workspaceID string) (int64, error) {
	n, err := a.e.opt.Store.ForgetTraces(ctx, workspaceID)
	if err != nil {
		return 0, err // nothing was deleted: keep serving what is still stored
	}
	a.e.sess.dropWorkspace(workspaceID)
	return n, nil
}

// SetHitFeedback sets or clears the feedback of a hit through the ledger (the one writer of memory_hits), then
// recounts the trace's votes (three downvotes demote its confidence, three upvotes promote it).
func (a *API) SetHitFeedback(ctx context.Context, workspaceID, hitID string, fb *string) (contract.MemoryHitView, error) {
	id, err := strconv.ParseInt(hitID, 10, 64)
	if err != nil || id <= 0 {
		return contract.MemoryHitView{}, contract.NewError(contract.CodeNotFound, "The memory hit does not exist.")
	}
	ref, err := a.e.opt.Ledger.SetHitFeedback(ctx, workspaceID, id, fb)
	if err != nil {
		return contract.MemoryHitView{}, err
	}
	if _, err := a.e.opt.Store.RecountTraceVotes(ctx, workspaceID, ref.TraceID); err != nil {
		a.e.opt.Log.Warn("memory votes not recounted", "err", err.Error())
	}
	row, err := a.e.opt.Store.MemoryHit(ctx, workspaceID, id)
	if err != nil {
		return contract.MemoryHitView{}, err
	}
	return hitView(row), nil
}

// SessionHits lists the hits of one session for the session detail; the hint is rendered from the trace now.
func (a *API) SessionHits(ctx context.Context, workspaceID, sessionID string) ([]contract.MemoryHitView, error) {
	rows, err := a.e.opt.Store.SessionMemoryHits(ctx, workspaceID, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]contract.MemoryHitView, len(rows))
	for i, r := range rows {
		out[i] = hitView(r)
	}
	return out, nil
}

func hitView(h store.HitRow) contract.MemoryHitView {
	v := contract.MemoryHitView{
		ID: strconv.FormatInt(h.ID, 10), CallID: h.CallID, TraceID: strconv.FormatInt(h.TraceID, 10), SourceSessionID: h.SourceSessionID,
		SimilarityBp: h.SimilarityBp, Mode: h.Mode, Injected: h.Injected, Feedback: h.Feedback, EstSavedUsd: h.EstSaved,
	}
	if h.Approach != "" || h.Fix != "" {
		v.Hint = RenderHint(h.Approach, h.Fix, h.Files)
	}
	return v
}

// pruneEvery is how often the retention job runs.
const pruneEvery = time.Hour

// pruneLoop deletes traces unused for the retention period (MEMORY_TRACE_RETENTION_DAYS) and old failure records.
func (e *Engine) pruneLoop(ctx context.Context) {
	if e.opt.RetentionDays <= 0 {
		return
	}
	t := time.NewTimer(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.prune(ctx)
			t.Reset(pruneEvery)
		}
	}
}

// prune runs one retention pass.
func (e *Engine) prune(ctx context.Context) {
	cutoff := e.opt.Now().AddDate(0, 0, -e.opt.RetentionDays)
	n, err := e.opt.Store.PruneMemory(ctx, cutoff)
	if err != nil {
		e.opt.Log.Warn("memory prune failed", "err", err.Error())
		return
	}
	e.pruned.Add(n)
}
