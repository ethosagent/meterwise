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

// Editor edits provider JSON for notes and blocks. The internal/wire package implements it; archtest H3
// forbids this pure package from importing wire, so the host injects it (wire.Editor{}).
type Editor interface {
	AppendNotes(w tollgate.WireFormat, body []byte, notes []string) ([]byte, error)
	BlockResponse(w tollgate.WireFormat, stream bool, text string) (*tollgate.Response, error)
}

// errNoEditor is what a note or block attempt reports when no Editor was injected: the monitors then
// only observe.
var errNoEditor = errors.New("monitors: no wire editor injected")

// noEditor is the Editor used when Options.Editor is nil.
type noEditor struct{}

func (noEditor) AppendNotes(tollgate.WireFormat, []byte, []string) ([]byte, error) {
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
	ctr  *counters

	skipped, notesNotAdopted, blocks, blocksDegraded, blocksCapped, notesInjected, notesDropped, unfoldSkips *atomic.Int64
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
	ctr := newCounters()
	h := &Hook{opt: o, cfgs: newConfigMemo(), sess: newSessStore(o.MaxSessions), ctr: ctr,
		skipped: ctr.of(ctrSkipped), notesNotAdopted: ctr.of(ctrNotesNotAdopted), blocks: ctr.of(ctrBlocks),
		notesInjected: ctr.of(ctrNotesInjected), notesDropped: ctr.of(ctrNotesDropped),
		unfoldSkips: ctr.of(ctrUnfoldSkips), blocksDegraded: ctr.of(ctrBlocksDegraded), blocksCapped: ctr.of(ctrBlocksCapped)}
	return h
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

// sessKey names a session's note state; Before computes it once per call and keeps it in the call state.
func sessKey(tenant, session string) string { return tenant + "\x00" + session }

// metaOf is what history left in the call's Meta for the monitors: the verdict collection and the ring.
func metaOf(c *tollgate.Call) (*contract.CallVerdicts, contract.RingView) {
	vs, _ := c.Meta[contract.MetaMonitors].(*contract.CallVerdicts)
	ring, _ := c.Meta[contract.MetaRing].(contract.RingView)
	return vs, ring
}

// Before runs the history detectors over the ring, then blocks, notes or only observes per the
// workspace config. It does no I/O. It writes the verdicts through the pointer history left in Meta,
// as its last statement and only while the chain still waits for it (rulings R3, and gap 1 of the
// interface: a short-circuiting hook's own Call copy is not adopted).
func (h *Hook) Before(ctx context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Provider.Wire == tollgate.WirePassthrough || len(c.Messages) == 0 {
		return nil, nil
	}
	vs, ring := metaOf(c)
	if vs == nil || ring == nil || h.late(ctx) {
		return nil, nil
	}
	cfg := h.cfgs.get(c.Tenant.Attrs[AttrMonitors])
	st := &callState{cfg: cfg, key: sessKey(c.Tenant.ID, c.Session.ID)}
	c.Meta[metaState] = st
	fired := h.detect(c, ring, cfg)
	if h.late(ctx) { // checked again before anything is taken from the session: queued notes stay queued (m2)
		return nil, nil
	}
	p := h.decide(c, st, fired)
	if ctx.Err() != nil { // the chain stopped waiting: nothing is delivered, so nothing is used up (S2)
		h.giveBack(st.key, p.held)
		return nil, nil
	}
	h.commit(st, p)
	if p.body != nil {
		c.Body = p.body
	}
	st.noted = p.notes
	for _, v := range p.verdicts {
		if vs.Add(v) {
			h.ctr.of(firesName(v.Monitor)).Add(1)
		}
	}
	vs.AddNotes(p.notes)
	return p.resp, nil
}

// late reports, and counts, a chain that no longer waits for this hook: its context is done, or its
// deadline is nearer than MinDeadline.
func (h *Hook) late(ctx context.Context) bool {
	if ctx.Err() != nil || contract.NearDeadline(ctx, h.opt.Now(), h.opt.MinDeadline) {
		h.skipped.Add(1)
		return true
	}
	return false
}

// detect runs the Before detectors in registry order and fills each verdict's estimate basis.
func (h *Hook) detect(c *tollgate.Call, ring contract.RingView, cfg contract.MonitorConfig) []contract.MonitorVerdict {
	var out []contract.MonitorVerdict
	unfolded := unfoldedCall(c, ring)
	for _, d := range beforeDetectors {
		if v, ok := d.Detect(c, ring, cfg); ok {
			if unfolded && (v.Monitor == contract.MonitorDuplicateToolCall || v.Monitor == contract.MonitorRepeatedRead) {
				h.unfoldSkips.Add(1) // the repeat is the model asking for a result the trim hook folded
				continue
			}
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

// unfoldedCall reports whether the tool call whose result is pending is one the trim hook listed in
// Meta[MetaTrimUnfold]: its earlier result was folded, so asking again is not waste.
func unfoldedCall(c *tollgate.Call, ring contract.RingView) bool {
	var u contract.TrimUnfold
	switch v := c.Meta[contract.MetaTrimUnfold].(type) {
	case contract.TrimUnfold:
		u = v
	case *contract.TrimUnfold:
		if v != nil {
			u = *v
		}
	}
	calls := ring.Calls()
	if len(u.ArgsHashes) == 0 || len(calls) == 0 {
		return false
	}
	for _, h := range calls[len(calls)-1].ArgsHashes {
		if slices.Contains(u.ArgsHashes, h) {
			return true
		}
	}
	return false
}

// basisOf is what the estimator needs to price a verdict: the previous call's input tokens, how much
// the body grew since, and the session's median output. It is a lower bound of the basis the real numbers
// would give (ADR 0024). The first two come only from an entry that is the call just before this one (index
// Session.Calls-1) and whose usage is known; when it is unknown or absent (a catch-up the deadline skipped, a
// request that was not aligned) both are 0. The output part is the largest value the median provably reaches.
func basisOf(c *tollgate.Call, calls []contract.CallSummary) contract.EstBasis {
	var b contract.EstBasis
	n := len(calls)
	if n > 0 && calls[n-1].Index == c.Session.Calls-1 && known(calls[n-1]) {
		last := calls[n-1]
		b.PriorInputTokens = last.Input
		b.BodyDeltaBytes = max(0, int64(len(c.Body)-last.BodyBytes))
	}
	b.MedianOutputTokens = outputFloor(calls, c.Session.Calls)
	return b
}

// known reports whether an entry holds a call's real numbers. A real call never has zero input, so a zero
// is unknown whatever the flag says (history leaves it false; this keeps the bound if one slips through).
func known(s contract.CallSummary) bool { return s.UsageKnown && s.Input > 0 }

// outputFloor is a lower bound of the median over the calls with output, whatever the unknown entries held.
// The ring's window is the call indexes [max(1, n-RingDepth), n-1] for the current call n; an index with no
// entry (never added, skipped, or a call that failed) counts as unknown like an entry with unknown usage.
// With kp known positive outputs (sorted K) and u unknown entries, the exact median sits at position
// (kp+k)/2 of kp+k values for some k <= u of them positive. At least one known value at or below it is
// K[(kp+k)/2-k], so K[(kp+u)/2-u] bounds it for every k; below index 0 nothing is known and the floor is 0.
// With no unknown entry this is the plain median.
func outputFloor(calls []contract.CallSummary, n int) int64 {
	lo := max(1, n-contract.RingDepth)
	var outs []int64
	knownCalls := 0
	for _, s := range calls {
		if s.Index < lo || s.Index >= n || !known(s) {
			continue
		}
		knownCalls++
		if s.Output > 0 {
			outs = append(outs, s.Output)
		}
	}
	unknown := max(0, n-lo) - knownCalls
	i := (len(outs)+unknown)/2 - unknown
	if i < 0 || i >= len(outs) {
		return 0
	}
	slices.Sort(outs)
	return outs[i]
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
		if p != "" && hashID(c, contract.HashString(p)) == pathHash {
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
	vs, ring := metaOf(c)
	if vs == nil || ring == nil || !c.Stream || m.Finish != tollgate.FinishOK || m.Status >= 400 || st.empty() {
		return
	}
	for _, d := range streamDetectors {
		if v, ok := d.Detect(c, ring, st.cfg); ok {
			h.recordStream(st, vs, v)
		}
	}
}

func (h *Hook) recordStream(st *callState, vs *contract.CallVerdicts, v contract.MonitorVerdict) {
	v.Action = contract.ModeObserve
	if v.Mode != contract.ModeObserve && h.sess.queue(st.key, dedupKey(v), noteText(v)) {
		v.Action = contract.ModeNote
	}
	if vs.Add(v) {
		h.ctr.of(firesName(v.Monitor)).Add(1)
	}
}

// OnSessionClosed drops the session's note state.
func (h *Hook) OnSessionClosed(_ context.Context, s tollgate.Session) {
	h.sess.remove(sessKey(s.TenantID, s.ID))
}

// Status is the hook's counters for /statusz.
func (h *Hook) Status() map[string]any {
	m := map[string]any{"sessions": h.sess.size(), "sessions_evicted": h.sess.evicted.Load()}
	h.ctr.fill(m)
	return m
}
