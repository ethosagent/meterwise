package ledger

import (
	"cmp"
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// attachSavers turns what the trim and router hooks left in Meta (a TrimResult, a RouteDecision) and the memory
// hint into the call's saver fields (S0.8). The ledger stays the one writer (R5): the hooks only report.
//
// A blocked call, or one with no usage at all, never reached the upstream, so it records nothing here (but see the
// refused routed call below). Trim tokens
// follow R8 (the request's own measured bytes-per-token, else a quarter), and are certain only for OpenAI chat with
// reported input tokens and a body that was really rewritten; everything else is likely. Router savings are the price
// difference at the call's real usage and need both models priced. Observe records the would-save in the same
// columns with its action, so it stays out of both savings numbers (non-negotiable 5).
func (h *Hook) attachSavers(c *tollgate.Call, m tollgate.Metric, rec *contract.CallRecord) {
	noUsage := m.Usage.InputTokens <= 0 && m.Usage.OutputTokens <= 0 && m.Usage.CachedInputTokens <= 0 && m.Usage.ReasoningTokens <= 0
	// A routed call the upstream answered with an error (400/403/404 on the cheaper model, say) has no usage but is
	// still the router's decision, and the router stats count it as unavailable, so only the route is recorded for it
	// (saved 0).
	refused := noUsage && m.Status >= 400
	if rec.Blocked || (noUsage && !refused) {
		return
	}
	if !refused {
		h.attachTrimAndHint(c, rec)
	}
	if rd, ok := routeOf(c.Meta[contract.MetaRouter]); ok {
		h.attachRoute(rd, rec)
	} else if c.Meta[contract.MetaRouter] != nil {
		h.q.invalidSaverMeta.Add(1)
	}
}

func (h *Hook) attachTrimAndHint(c *tollgate.Call, rec *contract.CallRecord) {
	if tr, ok := trimOf(c.Meta[contract.MetaTrim]); ok {
		h.attachTrim(c, tr, rec)
	} else if c.Meta[contract.MetaTrim] != nil {
		h.q.invalidSaverMeta.Add(1) // a value of the wrong Go type is dropped, never silently
	}
	if mh, ok := hintOf(c.Meta[contract.MetaMemory]); ok && mh.Injected {
		rec.HintInjected = true
	} else if !ok && c.Meta[contract.MetaMemory] != nil {
		h.q.invalidSaverMeta.Add(1)
	}
}

func trimOf(v any) (contract.TrimResult, bool) {
	switch t := v.(type) {
	case contract.TrimResult:
		return t, true
	case *contract.TrimResult:
		if t != nil {
			return *t, true
		}
	}
	return contract.TrimResult{}, false
}

func routeOf(v any) (contract.RouteDecision, bool) {
	switch t := v.(type) {
	case contract.RouteDecision:
		return t, true
	case *contract.RouteDecision:
		if t != nil {
			return *t, true
		}
	}
	return contract.RouteDecision{}, false
}

func hintOf(v any) (contract.MemoryHint, bool) {
	switch t := v.(type) {
	case contract.MemoryHint:
		return t, true
	case *contract.MemoryHint:
		if t != nil {
			return *t, true
		}
	}
	return contract.MemoryHint{}, false
}

func (h *Hook) attachTrim(c *tollgate.Call, tr contract.TrimResult, rec *contract.CallRecord) {
	if tr.Action != contract.TrimActed && tr.Action != contract.TrimObserve {
		h.q.invalidSaverMeta.Add(1)
		return
	}
	sent := tr.BodyAfter
	if tr.Action == contract.TrimObserve {
		sent = tr.BodyBefore
	}
	tokens := contract.TrimTokens(int64(tr.BodyBefore-tr.BodyAfter), int64(sent), rec.Usage.Input)
	conf := contract.ConfidenceLikely
	if tr.Action == contract.TrimActed && c.Provider.Wire == tollgate.WireOpenAIChat && rec.Usage.Input > 0 {
		conf = contract.ConfidenceCertain
	}
	action := tr.Action
	rec.TrimAction, rec.TrimConfidence, rec.TrimTokens = &action, &conf, tokens
	if tokens > 0 {
		if cost, ok := h.opt.Pricer.Cost(rec.Provider, rec.Model, contract.Usage{Input: tokens}, rec.StartedAt); ok {
			rec.SavedUSDTrim = cost
		} else {
			h.q.trimUnpriced.Add(1)
		}
	}
	rec.TrimEvents = trimEvents(tr, tokens, rec)
}

// trimEvents is one row per non-reverted stage (the first of a repeated stage name), the call's trim tokens split
// pro rata by the bytes each stage saved; the last stage takes the rounding so the rows sum to the call's tokens.
func trimEvents(tr contract.TrimResult, tokens int64, rec *contract.CallRecord) []contract.TrimEventRecord {
	var stages []contract.TrimStageResult
	var total int64
	for _, s := range tr.Stages {
		if s.Reverted || !s.Stage.Valid() || slices.ContainsFunc(stages, func(o contract.TrimStageResult) bool { return o.Stage == s.Stage }) {
			continue
		}
		stages = append(stages, s)
		total += int64(max(s.BytesBefore-s.BytesAfter, 0))
	}
	out := make([]contract.TrimEventRecord, 0, len(stages))
	var given int64
	for i, s := range stages {
		share := int64(0)
		switch {
		case total == 0:
		case i == len(stages)-1:
			share = tokens - given
		default:
			share = tokens * int64(max(s.BytesBefore-s.BytesAfter, 0)) / total
		}
		given += share
		out = append(out, contract.TrimEventRecord{
			OrgID: rec.OrgID, WorkspaceID: rec.WorkspaceID, SessionID: rec.SessionID, CallID: rec.ID, TS: rec.StartedAt, Stage: s.Stage,
			Tool: clean(s.Tool, maxNameLen), Regions: max(s.Regions, 0), BytesBefore: max(s.BytesBefore, 0), BytesAfter: max(s.BytesAfter, 0),
			EstTokens: max(share, 0), Reverted: false, Action: tr.Action,
		})
	}
	return out
}

func (h *Hook) attachRoute(rd contract.RouteDecision, rec *contract.CallRecord) {
	if rd.Action == contract.RouteHeld {
		return // a held route changed nothing
	}
	if rd.Action != contract.RouteRouted && rd.Action != contract.RouteObserve {
		h.q.invalidSaverMeta.Add(1)
		return
	}
	action := rd.Action
	rec.RouterAction = &action
	rec.RoutedFrom, rec.RoutedTo = clean(rd.From, maxFieldLen), clean(rd.To, maxFieldLen)
	rec.RouterScoreBp = new(min(max(rd.ScoreBp, 0), 10000))
	// routed: the call was priced at the target (the real bill), so the saving is source minus target. observe: the
	// call ran on its own model, so the would-save is that model minus the target.
	from := cmp.Or(rec.RoutedFrom, rec.Model)
	if rd.Action == contract.RouteObserve {
		from = rec.Model
	}
	cf, okFrom := h.opt.Pricer.Cost(rec.Provider, from, rec.Usage, rec.StartedAt)
	ct, okTo := h.opt.Pricer.Cost(rec.Provider, rec.RoutedTo, rec.Usage, rec.StartedAt)
	if !okFrom || !okTo {
		h.q.routerUnpriced.Add(1)
		return
	}
	rec.SavedUSDRouter = max(cf-ct, 0)
}
