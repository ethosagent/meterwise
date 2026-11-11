package router

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/wire"
)

// Reasons the router adds to contract's held vocabulary. They are never recorded in the ledger (a held call
// changes nothing), only stamped on Meta[router] and counted.
const (
	HeldScore      = "score"       // the score is above the threshold: the step is not simple enough
	HeldEditFailed = "edit_failed" // the body could not be edited, so it was left as it came
)

// UnavailableTTL is how long a pair stays off after an upstream refused a routed call (ruling A1).
const UnavailableTTL = time.Hour

// refused are the upstream statuses that mark a pair unavailable: the target is not served for this key (403, 404)
// or the request is not valid for it (400). The client sees that one error; later calls stay on the strong model.
func refused(status int) bool { return status == 400 || status == 403 || status == 404 }

// Options configures the hook. Only Tables is needed.
type Options struct {
	Tables      contract.RouterTables
	Weights     Weights          // zero takes DefaultWeights
	MaxSessions int              // bound of the per-session memory; default 100000
	Now         func() time.Time // default time.Now
}

// Hook is the router's tollgate Hook (Name "router"). It does no I/O and its only clock read is for the
// unavailable marks.
type Hook struct {
	tollgate.NoopHook
	opt  Options
	cfg  configs
	sess *sessions
	off  *unavailable

	routed, observed, held, editFailed, marked, markDropped, evicted atomic.Int64
	heldBy                                                           [8]atomic.Int64
}

// New builds a Hook.
func New(o Options) *Hook {
	if o.Weights == (Weights{}) {
		o.Weights = DefaultWeights
	}
	if o.MaxSessions <= 0 {
		o.MaxSessions = 100000
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Hook{opt: o, cfg: configs{defaults: o.Tables.Routes}, sess: newSessions(o.MaxSessions), off: newUnavailable(4096)}
}

// Name identifies the hook.
func (*Hook) Name() string { return "router" }

// AttrRouter is the Tenant.Attrs key of the workspace's router settings (wsauth.AttrRouter; pure packages may not
// import wsauth, so a test in dataplane holds the two equal).
const AttrRouter = "router"

const metaPair = "router.pair"

// Before decides the call and, in conservative or aggressive mode, swaps the model. It stamps Meta[router] with
// the decision whenever the workspace's mode is not off. It never short-circuits, so its copy of the call is adopted.
func (h *Hook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Provider.Wire != tollgate.WireOpenAIChat && c.Provider.Wire != tollgate.WireAnthropicMessages {
		return nil, nil
	}
	rc := h.cfg.resolve(c.Tenant.Attrs[AttrRouter])
	if rc.mode == contract.RouterModeOff {
		return nil, nil
	}
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	d, pair := h.decide(c, rc)
	c.Meta[contract.MetaRouter] = d
	if d.Action == contract.RouteHeld {
		h.held.Add(1)
		h.countHeld(d.Reason)
		return nil, nil
	}
	c.Meta[metaPair] = pairKey{tenant: c.Tenant.ID, provider: c.Provider.Name, from: pair.From, to: pair.To}
	if d.Action == contract.RouteObserve {
		h.observed.Add(1)
		return nil, nil
	}
	body, err := wire.SetModel(c.Provider.Wire, c.Body, pair.To)
	if err != nil {
		h.editFailed.Add(1)
		h.held.Add(1)
		h.countHeld(HeldEditFailed)
		delete(c.Meta, metaPair)
		c.Meta[contract.MetaRouter] = contract.RouteDecision{Mode: d.Mode, Action: contract.RouteHeld, From: d.From, To: d.To, ScoreBp: d.ScoreBp, Reason: HeldEditFailed}
		return nil, nil
	}
	c.Body, c.Model = body, pair.To
	h.routed.Add(1)
	h.remember(c)
	return nil, nil
}

// decide is the whole decision, in order: the model must have a pair, both ends priced, the pair not recently
// refused; then the score (first steps, escalation, a failing tool result, the threshold); last the capability
// guard, which is the only step that reads the request body.
func (h *Hook) decide(c *tollgate.Call, rc *resolved) (contract.RouteDecision, contract.RoutePair) {
	d := contract.RouteDecision{Mode: rc.mode, Action: contract.RouteHeld, From: c.Model}
	pair, ok := rc.match(c.Provider.Name, c.Model)
	escalated := h.escalated(c) // every call ages an escalation, whatever model it asked for
	if !ok {
		d.Reason = contract.HeldNotInMap
		return d, pair
	}
	d.To = pair.To
	t := h.opt.Tables
	prov := c.Provider.Name
	switch {
	case !t.Priced[TableKey(prov, pair.From)] || !t.Priced[TableKey(prov, pair.To)]:
		d.Reason = contract.HeldUnpriced
		return d, pair
	case h.off.blocked(pairKey{tenant: c.Tenant.ID, provider: prov, from: pair.From, to: pair.To}, h.opt.Now()):
		d.Reason = contract.HeldUnavailable
		return d, pair
	}
	res := h.opt.Weights.Score(h.input(c, escalated))
	d.ScoreBp = res.Bp
	switch {
	case res.Held != "":
		d.Reason = res.Held
		return d, pair
	case res.Bp > rc.threshold:
		d.Reason = HeldScore
		return d, pair
	}
	caps, known := t.Caps[TableKey(prov, pair.To)]
	f, err := wire.Features(c.Provider.Wire, c.Body)
	if !known || err != nil || !Allows(caps, f) {
		d.Reason = contract.HeldCapability
		return d, pair
	}
	d.Action = contract.RouteObserve
	if rc.mode != contract.RouterModeObserve {
		d.Action = contract.RouteRouted
	}
	d.Reason = ""
	return d, pair
}

// input gathers what the scorer reads: the ring history put in Meta, the request's newest tool results, whether
// memory injected a hint, and whether this call is forced strong.
func (h *Hook) input(c *tollgate.Call, escalated bool) Input {
	in := Input{CallIndex: max(c.Session.Calls, 1), Escalated: escalated, LastToolError: lastToolFailed(c.Messages)}
	if rv, ok := c.Meta[contract.MetaRing].(contract.RingView); ok && rv != nil {
		in.Ring = rv.Calls()
	}
	switch mh := c.Meta[contract.MetaMemory].(type) {
	case contract.MemoryHint:
		in.HintInjected = mh.Injected
	case *contract.MemoryHint:
		in.HintInjected = mh != nil && mh.Injected
	}
	return in
}

func (h *Hook) key(c *tollgate.Call) string { return c.Tenant.ID + "\x00" + c.Session.ID }

// escalated reads the previous routed call's verdicts and reports whether this call is forced strong. A verdict on
// that call (the monitors' Before and stream detectors have run by now) forces this call and the next one.
func (h *Hook) escalated(c *tollgate.Call) (forced bool) {
	if c.Session.ID == "" {
		return false
	}
	ev := h.sess.use(h.key(c), func(st *sessionState) {
		if st.pending != nil {
			if len(st.pending.Snapshot()) > 0 {
				st.forced = escalateCalls
			}
			st.pending = nil
		}
		if st.forced > 0 {
			st.forced--
			forced = true
		}
	})
	if ev {
		h.evicted.Add(1)
	}
	return forced
}

// remember keeps the routed call's verdict collection (history put it in Meta) for the next Before.
func (h *Hook) remember(c *tollgate.Call) {
	v, _ := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts)
	if v == nil || c.Session.ID == "" {
		return
	}
	if h.sess.use(h.key(c), func(st *sessionState) { st.pending = v }) {
		h.evicted.Add(1)
	}
}

// After marks the pair unavailable when the upstream refused a routed call.
func (h *Hook) After(_ context.Context, c *tollgate.Call, m tollgate.Metric) {
	d, ok := c.Meta[contract.MetaRouter].(contract.RouteDecision)
	if !ok || d.Action != contract.RouteRouted || !refused(m.Status) {
		return
	}
	k, ok := c.Meta[metaPair].(pairKey)
	if !ok {
		return
	}
	now := h.opt.Now()
	if h.off.mark(k, now, now.Add(UnavailableTTL)) {
		h.marked.Add(1)
	} else {
		h.markDropped.Add(1)
	}
}

// OnSessionClosed drops the session's escalation state.
func (h *Hook) OnSessionClosed(_ context.Context, s tollgate.Session) {
	h.sess.drop(s.TenantID + "\x00" + s.ID)
}

var heldReasons = [...]string{
	contract.HeldFirstSteps, contract.HeldEscalated, contract.HeldErrorRetry, contract.HeldCapability, contract.HeldNotInMap,
	contract.HeldUnpriced, contract.HeldUnavailable, HeldScore,
}

func (h *Hook) countHeld(reason string) {
	for i, r := range heldReasons {
		if r == reason {
			h.heldBy[i].Add(1)
			return
		}
	}
}

// Status is the hook's counters for /statusz: decisions by outcome and held reason, edits that failed, pairs
// marked unavailable (and marks dropped for lack of room), live sessions and evictions.
func (h *Hook) Status() map[string]any {
	by := map[string]any{}
	for i, r := range heldReasons {
		by[r] = h.heldBy[i].Load()
	}
	return map[string]any{
		"routed": h.routed.Load(), "observed": h.observed.Load(), "held": h.held.Load(), "held_by_reason": by,
		"edit_failed": h.editFailed.Load(), "unavailable_marked": h.marked.Load(), "unavailable_dropped": h.markDropped.Load(),
		"unavailable_now": h.off.size(h.opt.Now()), "sessions": h.sess.size(), "evicted": h.evicted.Load(),
	}
}
