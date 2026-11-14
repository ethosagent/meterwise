package memory

import (
	"context"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wire"
)

// Hook is memory's tollgate hook (Name "memory"). Before does no I/O: it reads the session's cached recall result
// under a short lock and, in hint mode, splices one note into the body (ruling R1). After feeds the session summary and
// starts recalls off the request path.
type Hook struct {
	tollgate.NoopHook
	e *Engine
}

// Name identifies the hook.
func (*Hook) Name() string { return "memory" }

func supported(w tollgate.WireFormat) bool {
	return w == tollgate.WireOpenAIChat || w == tollgate.WireAnthropicMessages
}

// gate is the preamble both hook points share: the call is on a supported wire, in a session, memory is not off for
// the workspace and capture is allowed. countPrivacy counts a privacy refusal (Before does, once per call). It does
// no I/O.
func (e *Engine) gate(c *tollgate.Call, countPrivacy bool) (contract.MemoryConfig, bool) {
	if !supported(c.Provider.Wire) || c.Session.ID == "" {
		return contract.MemoryConfig{}, false
	}
	cfg := e.config(c)
	if cfg.Mode == contract.MemoryModeOff {
		return cfg, false
	}
	if !e.mayCapture(c) {
		if countPrivacy {
			e.skippedPrivacy.Add(1)
		}
		return cfg, false
	}
	return cfg, true
}

// Before stamps Meta[memory] and, in hint mode, appends the one-line hint of the recalled trace. It never
// short-circuits, and on any wire error it leaves the body as it came and counts edit_failed.
func (h *Hook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	e := h.e
	cfg, ok := e.gate(c, true)
	if !ok {
		return nil, nil
	}
	cand := e.sess.next(c.Tenant.ID, c.Session.ID, cfg.MaxHintsPerSession, c.Session.Calls)
	if cand == nil {
		return nil, nil
	}
	mh := contract.MemoryHint{Mode: cfg.Mode, TraceID: cand.traceID, SimilarityBp: cand.simBp}
	if cfg.Mode == contract.MemoryModeHint {
		body, err := wire.AppendNotes(c.Provider.Wire, c.Body, []string{cand.hint})
		if err != nil {
			e.editFailed.Add(1)
			return nil, nil // not sent, so not a hit: it must not count as an observed one
		}
		c.Body, mh.Injected = body, true
		e.injected.Add(1)
	} else {
		e.observed.Add(1)
	}
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	c.Meta[contract.MetaMemory] = mh
	return nil, nil
}

// After folds the call into the session summary, starts a recall when one is due and notes a used trace.
func (h *Hook) After(ctx context.Context, c *tollgate.Call, _ tollgate.Metric) {
	e := h.e
	cfg, ok := e.gate(c, false)
	if !ok {
		return
	}
	if mh, ok := c.Meta[contract.MetaMemory].(contract.MemoryHint); ok && mh.Injected {
		ws, id := c.Tenant.ID, mh.TraceID
		e.jobs.enqueue(ctx, ws, kindTouch, func(ctx context.Context) error { return e.opt.Store.TouchTrace(ctx, ws, id) })
	}
	if start, in := e.sess.observe(c, cfg, e.opt.Now()); start {
		if !e.jobs.enqueue(ctx, in.ws, kindRecall, func(ctx context.Context) error { return e.runRecall(ctx, in) }) {
			e.sess.setCandidate(in.ws, in.id, nil)
		}
	}
}

// OnSessionClosed does nothing: the state is dropped when the ledger reports the close (Engine.OnClosed), because
// the outcome is the ledger's to decide.
func (*Hook) OnSessionClosed(context.Context, tollgate.Session) {}

// Status is the counters for /statusz: no text, no ids.
func (h *Hook) Status() map[string]any {
	e := h.e
	out := e.jobs.status()
	out["enabled"] = e.Keyed()
	out["sessions"] = e.sess.size()
	out["sessions_evicted"] = e.sess.evictions()
	out["hints_injected"] = e.injected.Load()
	out["hits_observed"] = e.observed.Load()
	out["edit_failed"] = e.editFailed.Load()
	out["recalls"] = e.recalls.Load()
	out["recalls_with_hit"] = e.recallHits.Load()
	out["skipped_privacy"] = e.skippedPrivacy.Load()
	out["skipped_budget"] = e.skippedBudget.Load()
	budgetCap, paused, spent := e.budgetState()
	out["budget_cap_micro_usd"], out["budget_paused_workspaces"], out["budget_spent_micro_usd"] = int64(budgetCap), paused, int64(spent)
	out["closed_without_state"] = e.noState.Load()
	out["traces_pruned"] = e.pruned.Load()
	return out
}
