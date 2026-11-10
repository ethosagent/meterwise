package ledger

import (
	"slices"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Estimator turns one verdict into the tokens it saves (or would save) and how sure that is. The savings
// package supplies it (savings.Estimate) when the host wires the ledger; the ledger does not import it, so
// the default is an estimator that finds nothing: zero tokens, confidence none.
type Estimator func(v contract.MonitorVerdict) (contract.Usage, contract.Confidence)

func zeroEstimate(contract.MonitorVerdict) (contract.Usage, contract.Confidence) {
	return contract.Usage{}, contract.ConfidenceNone
}

// attachVerdicts seals the call's verdicts (history put the pointer in Meta, the monitors wrote through it)
// and prices them onto rec: Verdicts, Blocked and NotesInjected (rulings R3, R6, R7).
//
// A block verdict on a call that already has usage lost a race with the upstream (the call went out): it is
// downgraded to observe and counted, so it can never inflate a savings number. Every field that reaches a text
// column is cleaned, a verdict with a name, mode or action outside the fixed sets is dropped and counted
// (it would violate the table's CHECKs and poison the batch), and a plain path is kept only when the
// workspace allows capture.
func (h *Hook) attachVerdicts(c *tollgate.Call, m tollgate.Metric, rec *contract.CallRecord) {
	cv, _ := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts)
	if cv == nil {
		return
	}
	list, notes := cv.Seal()
	rec.NotesInjected = notes
	late := m.Usage.InputTokens > 0 || m.Usage.OutputTokens > 0 || m.Usage.CachedInputTokens > 0 || m.Usage.ReasoningTokens > 0
	capture := h.mayCapture(c)
	for _, v := range list {
		if !validVerdict(v) {
			h.q.invalidVerdicts.Add(1)
			continue
		}
		if v.Action == contract.ModeBlock && late {
			v.Action = contract.ModeObserve
			h.q.blocksDowngraded.Add(1)
		}
		v.Evidence = cleanEvidence(v.Evidence, capture)
		rec.Verdicts = append(rec.Verdicts, h.price(v, rec))
		rec.Blocked = rec.Blocked || v.Action == contract.ModeBlock
	}
	attributeOnce(rec.Verdicts)
}

// attributeOnce keeps one estimate per call (M1): every verdict of a call describes the same avoided call,
// so adding them would count it several times. The estimate stays on, in order of preference, the verdict
// with the largest certain estimate that acted (a block), else the largest likely one that acted (a note),
// else the largest would-save of an observed one. Every other verdict records 0 tokens and 0 USD; its
// confidence and evidence stay, so the event still shows what fired.
func attributeOnce(vs []contract.PricedVerdict) {
	best, rank := -1, 0
	for i, v := range vs {
		r := attributionRank(v)
		if r == 0 {
			continue
		}
		if r > rank || (r == rank && v.EstTokens > vs[best].EstTokens) {
			best, rank = i, r
		}
	}
	for i := range vs {
		if i != best {
			vs[i].EstTokens, vs[i].EstUSD = 0, 0
		}
	}
}

// attributionRank orders the classes a call's one estimate can go to: 3 acted and certain, 2 acted and
// likely, 1 observed (would-save), 0 no estimate.
func attributionRank(v contract.PricedVerdict) int {
	switch {
	case v.EstTokens <= 0 && v.EstUSD <= 0:
		return 0
	case v.Action == contract.ModeObserve:
		return 1
	case v.Confidence == contract.ConfidenceCertain:
		return 3
	case v.Confidence == contract.ConfidenceLikely:
		return 2
	}
	return 0
}

// price runs the estimator and prices its tokens at the call's start. A model with no price keeps the tokens
// and a zero USD value (the estimate is flagged by the unpriced call, not invented).
func (h *Hook) price(v contract.MonitorVerdict, rec *contract.CallRecord) contract.PricedVerdict {
	u, conf := h.opt.Estimator(v)
	u = nonNeg(u)
	if conf != contract.ConfidenceCertain && conf != contract.ConfidenceLikely {
		conf = contract.ConfidenceNone
	}
	pv := contract.PricedVerdict{MonitorVerdict: v, EstTokens: u.Input + u.Output, Confidence: conf}
	if cost, ok := h.opt.Pricer.Cost(rec.Provider, rec.Model, u, rec.StartedAt); ok {
		pv.EstUSD = cost
	}
	return pv
}

func validVerdict(v contract.MonitorVerdict) bool {
	return slices.Contains(contract.MonitorNames(), v.Monitor) && v.Mode.Valid() && v.Action.Valid()
}

// cleanEvidence makes the evidence safe for a jsonb column (no NUL, valid UTF-8, bounded) and drops the plain
// path unless capture is allowed. Hashes, counts and phrases are all that is ever stored otherwise.
func cleanEvidence(e contract.MonitorEvidence, capture bool) contract.MonitorEvidence {
	e.Tool = clean(e.Tool, maxNameLen)
	e.ArgsHash = clean(e.ArgsHash, maxNameLen)
	e.PathHash = clean(e.PathHash, maxNameLen)
	e.Hash = clean(e.Hash, maxNameLen)
	if capture {
		e.Path = clean(e.Path, maxFieldLen)
	} else {
		e.Path = ""
	}
	// the slices belong to the monitors' verdict: copy before bounding or cleaning
	e.Phrases = slices.Clone(e.Phrases[:min(len(e.Phrases), maxToolNames)])
	for i, p := range e.Phrases {
		e.Phrases[i] = clean(p, maxNameLen)
	}
	e.MatchedCalls = slices.Clone(e.MatchedCalls[:min(len(e.MatchedCalls), maxToolNames)])
	return e
}
