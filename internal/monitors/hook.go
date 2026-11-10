package monitors

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Editor edits provider JSON for notes and blocks. The monitors/wire package implements it; archtest H3
// forbids this pure package from importing wire, so the host injects it (wire.Editor{}).
type Editor interface {
	AppendNote(w tollgate.WireFormat, body []byte, note string) ([]byte, error)
	BlockResponse(w tollgate.WireFormat, stream bool, text string) (*tollgate.Response, error)
}

// errNoEditor is what a note or block attempt reports when no Editor was injected: the monitors then
// only observe.
var errNoEditor = errors.New("monitors: no wire editor injected")

// noEditor is the Editor used when Options.Editor is nil.
type noEditor struct{}

func (noEditor) AppendNote(tollgate.WireFormat, []byte, string) ([]byte, error) {
	return nil, errNoEditor
}

func (noEditor) BlockResponse(tollgate.WireFormat, bool, string) (*tollgate.Response, error) {
	return nil, errNoEditor
}

// Options configures the Hook. The zero value is usable.
type Options struct {
	// Editor splices notes and builds block responses. Required for anything but observing; nil means
	// every note and block degrades to observe (counted).
	Editor Editor
	// PrivacyMode is the proxy-wide PRIVACY_MODE. A plain file path goes into evidence only when it is
	// false and the workspace policy opts in to capture (the ledger's rule, ruling R7).
	PrivacyMode bool
	// MaxSessions bounds the per-session note state. Default 100000.
	MaxSessions int
	// Now reads the clock for the deadline check only. Default time.Now.
	Now func() time.Time
	// MinDeadline: when the chain's deadline is nearer than this, Before skips itself. Default 5ms.
	MinDeadline time.Duration
}

// Hook is the monitors tollgate Hook (Name "monitors").
type Hook struct {
	tollgate.NoopHook
	opt  Options
	cfgs *configMemo
	sess *sessStore

	fires           [6]atomic.Int64
	skipped         atomic.Int64
	notesNotAdopted atomic.Int64
	blocksDegraded  atomic.Int64
	blocksCapped    atomic.Int64
	blocks          atomic.Int64
	notesInjected   atomic.Int64
	notesDropped    atomic.Int64
}

// New builds a Hook.
func New(o Options) *Hook {
	if o.MaxSessions <= 0 {
		o.MaxSessions = 100000
	}
	if o.Editor == nil {
		o.Editor = noEditor{}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.MinDeadline <= 0 {
		o.MinDeadline = 5 * time.Millisecond
	}
	return &Hook{opt: o, cfgs: newConfigMemo(), sess: newSessStore(o.MaxSessions)}
}

// Name identifies the hook.
func (*Hook) Name() string { return "monitors" }

var beforeDetectors, streamDetectors = split(Registry())

func split(all []Detector) (before, stream []Detector) {
	for _, d := range all {
		if d.Stream {
			stream = append(stream, d)
		} else {
			before = append(before, d)
		}
	}
	return before, stream
}

func sessKey(c *tollgate.Call) string { return c.Tenant.ID + "\x00" + c.Session.ID }

// Before runs the history detectors over the ring, then blocks, notes or only observes per the
// workspace config. It does no I/O. It writes the verdicts through the pointer history left in Meta,
// as its last statement and only while the chain still waits for it (rulings R3, and gap 1 of the
// interface: a short-circuiting hook's own Call copy is not adopted).
func (h *Hook) Before(ctx context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Provider.Wire == tollgate.WirePassthrough || len(c.Messages) == 0 {
		return nil, nil
	}
	vs, _ := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts)
	ring, _ := c.Meta[contract.MetaRing].(contract.RingView)
	if vs == nil || ring == nil {
		return nil, nil
	}
	if d, ok := ctx.Deadline(); ok && d.Sub(h.opt.Now()) < h.opt.MinDeadline {
		h.skipped.Add(1)
		return nil, nil
	}
	cfg := h.cfgs.get(c.Tenant.Attrs[AttrMonitors])
	st := &callState{cfg: cfg}
	c.Meta[metaState] = st
	p := h.decide(c, h.detect(c, ring, cfg), cfg)
	if ctx.Err() != nil {
		return nil, nil
	}
	if p.body != nil {
		c.Body = p.body
	}
	st.noted = p.notes
	for _, v := range p.verdicts {
		if vs.Add(v) {
			h.countFire(v.Monitor)
		}
	}
	vs.AddNotes(p.notes)
	return p.resp, nil
}

func (h *Hook) countFire(m contract.MonitorName) {
	if i := slices.Index(contract.MonitorNames(), m); i >= 0 {
		h.fires[i].Add(1)
	}
}

// detect runs the Before detectors in registry order and fills each verdict's estimate basis.
func (h *Hook) detect(c *tollgate.Call, ring contract.RingView, cfg contract.MonitorConfig) []contract.MonitorVerdict {
	var out []contract.MonitorVerdict
	for _, d := range beforeDetectors {
		if v, ok := d.Detect(c, ring, cfg); ok {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	b := basisOf(c, ring.Calls())
	capture := h.mayCapture(c)
	for i := range out {
		out[i].Basis = b
		if capture && out[i].Monitor == contract.MonitorRepeatedRead {
			out[i].Evidence.Path = pathFor(c, out[i].Evidence.PathHash)
		}
	}
	return out
}

// basisOf is what the estimator needs to price a verdict: the previous call's input tokens, how much
// the body grew since, and the session's median output.
func basisOf(c *tollgate.Call, calls []contract.CallSummary) contract.EstBasis {
	var b contract.EstBasis
	var outs []int64
	for _, s := range calls {
		if s.Output > 0 {
			outs = append(outs, s.Output)
		}
	}
	if n := len(calls); n > 0 {
		b.PriorInputTokens = calls[n-1].Input
		b.BodyDeltaBytes = max(0, int64(len(c.Body)-calls[n-1].BodyBytes))
	}
	if len(outs) > 0 {
		slices.Sort(outs)
		b.MedianOutputTokens = outs[len(outs)/2]
	}
	return b
}

func (h *Hook) mayCapture(c *tollgate.Call) bool {
	p := c.Tenant.Policy
	if p == nil || !p.Capture || h.opt.PrivacyMode {
		return false
	}
	return p.PrivacyMode == nil || !*p.PrivacyMode
}

// pathFor finds the plain path behind a path hash in the newest assistant turn (capture only).
func pathFor(c *tollgate.Call, pathHash string) string {
	i := lastAssistant(c.Messages)
	if i < 0 {
		return ""
	}
	for _, tc := range c.Messages[i].ToolCalls {
		p := contract.ToolPath(tc.Arguments)
		if p != "" && contract.HashID(c.Tenant.ID, contract.HashValue(contract.HashString(p))) == pathHash {
			return p
		}
	}
	return ""
}

// OnEvent collects stream text and tool names for the stream detectors, off the client path.
func (*Hook) OnEvent(_ context.Context, c *tollgate.Call, e tollgate.Event) {
	if st := stateOf(c); st != nil {
		st.event(e)
	}
}

// After runs the stream detectors over what OnEvent collected and records their verdicts where they
// fired (ruling R8); in note mode the note waits for the session's next call.
func (h *Hook) After(_ context.Context, c *tollgate.Call, m tollgate.Metric) {
	st := stateOf(c)
	if st == nil {
		return
	}
	if st.noted > 0 && !bytes.Contains(c.Body, []byte(notePrefix)) {
		h.notesNotAdopted.Add(1)
	}
	vs, _ := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts)
	ring, _ := c.Meta[contract.MetaRing].(contract.RingView)
	if vs == nil || ring == nil || !c.Stream || m.Finish != tollgate.FinishOK || m.Status >= 400 {
		return
	}
	for _, d := range streamDetectors {
		if v, ok := d.Detect(c, ring, st.cfg); ok {
			h.recordStream(c, vs, v)
		}
	}
}

func (h *Hook) recordStream(c *tollgate.Call, vs *contract.CallVerdicts, v contract.MonitorVerdict) {
	v.Action = contract.ModeObserve
	if v.Mode != contract.ModeObserve {
		key := sessKey(c)
		if h.sess.markSeen(key, dedupKey(v)) {
			h.sess.queue(key, noteText(v))
			v.Action = contract.ModeNote
		}
	}
	if vs.Add(v) {
		h.countFire(v.Monitor)
	}
}

// OnSessionClosed drops the session's note state.
func (h *Hook) OnSessionClosed(_ context.Context, s tollgate.Session) {
	h.sess.remove(s.TenantID + "\x00" + s.ID)
}

// Status is the hook's counters for /statusz.
func (h *Hook) Status() map[string]any {
	m := map[string]any{
		"skipped_deadline": h.skipped.Load(), "notes_not_adopted": h.notesNotAdopted.Load(),
		"blocks_degraded": h.blocksDegraded.Load(), "blocks_capped": h.blocksCapped.Load(), "blocks": h.blocks.Load(),
		"notes_injected": h.notesInjected.Load(), "notes_dropped": h.notesDropped.Load(),
		"sessions": h.sess.size(), "sessions_evicted": h.sess.evicted.Load(),
	}
	for i, n := range contract.MonitorNames() {
		m["fires_"+string(n)] = h.fires[i].Load()
	}
	return m
}
