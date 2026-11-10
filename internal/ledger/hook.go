package ledger

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/store"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// metaState is the one Meta key the hook stamps on a call (scratch space shared by extensions for that call).
const metaState = "ledger.state"

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// tenantOK reports whether the call's workspace and org ids are canonical uuids. wsauth checks them once per
// cache fill and says so in Attrs[AttrTenantOK]; a tenant without the mark (the dev tenant, tests, any other
// authenticator) falls back to the pattern match.
func tenantOK(t tollgate.Tenant) bool {
	if t.Attrs[wsauth.AttrTenantOK] == "true" {
		return true
	}
	return uuidRe.MatchString(t.ID) && uuidRe.MatchString(t.Attrs["org_id"])
}

// Hook is the ledger's tollgate Hook (Name "ledger"). Create it with New; it owns a batcher
// goroutine that Close stops after a final flush.
type Hook struct {
	tollgate.NoopHook
	opt  Options
	q    *queue
	warn sync.Map // models already reported as unpriced
	nwrn atomic.Int64
}

// callState is everything the hook keeps for one call (a single Meta entry): what Before stamped (the
// time the call reached the hook and its 1-based position in the session) and what OnEvent learns from
// the stream (tool names, whether the assistant wrote text). After reads it.
type callState struct {
	start   time.Time
	index   int
	preview string
	text    atomic.Bool // set once by the first non-blank text delta; later deltas return at once
	mu      sync.Mutex  // guards tools
	tools   []string
}

// New builds the hook and starts its batcher. Pricer and Writer are required. ctx supplies values
// (not cancellation) to the batcher; stop it with Close.
func New(ctx context.Context, o Options) (*Hook, error) {
	o, err := o.withDefaults()
	if err != nil {
		return nil, err
	}
	h := &Hook{opt: o}
	h.q = newQueue(o)
	h.q.start(ctx)
	return h, nil
}

// Name identifies the hook.
func (*Hook) Name() string { return "ledger" }

// Before stamps the call and, only when capture is on, remembers the first user message preview. It
// does no I/O; it allocates the state record and, when the call has none yet, the Meta map.
func (h *Hook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	st := &callState{start: h.opt.Now(), index: c.Session.Calls}
	if c.Session.Calls <= 1 && h.mayCapture(c) {
		st.preview = previewOf(c.Messages)
	}
	c.Meta[metaState] = st
	return nil, nil
}

// mayCapture: the workspace opted in to capture, privacy mode is off globally, and the workspace
// did not switch it on for itself. The strictest of the three wins.
func (h *Hook) mayCapture(c *tollgate.Call) bool {
	p := c.Tenant.Policy
	if p == nil || !p.Capture || *h.opt.PrivacyMode {
		return false
	}
	return p.PrivacyMode == nil || !*p.PrivacyMode
}

// OnEvent learns tool-call names and whether the assistant produced text from the neutral stream
// events (never from provider JSON). It is off the client path and does no I/O.
func (h *Hook) OnEvent(_ context.Context, c *tollgate.Call, e tollgate.Event) {
	st, _ := c.Meta[metaState].(*callState)
	if st == nil {
		return
	}
	switch e.Kind {
	case tollgate.EventTextDelta:
		if !st.text.Load() && strings.TrimSpace(e.Text) != "" {
			st.text.Store(true)
		}
	case tollgate.EventToolCallDelta:
		if e.ToolCall != nil && e.ToolCall.Name != "" {
			st.mu.Lock()
			if len(st.tools) < maxToolNames {
				st.tools = append(st.tools, clean(e.ToolCall.Name, maxNameLen))
			}
			st.mu.Unlock()
		}
	}
}

// After enqueues one record and returns at once; a full queue drops it and counts the drop.
func (h *Hook) After(ctx context.Context, c *tollgate.Call, m tollgate.Metric) {
	// Single org: a row filed under any other org could never be read back, and its workspace's
	// last_call_at would silently not move. Refuse and count it instead.
	if org := c.Tenant.Attrs["org_id"]; org != store.OrgID() && (c.Tenant.Attrs[wsauth.AttrTenantOK] == "true" || uuidRe.MatchString(org)) {
		h.q.wrongOrg.Add(1)
		return
	}
	// A call refused before it reached the upstream (admission 429, provider or CIDR 403, unknown or
	// retired id 404/410) is not a call: no row, no session, no last_call_at. A rejection of 500 or more
	// (extension fault, upstream unreachable) did get as far as the upstream and is still recorded.
	if m.Finish == tollgate.FinishRejected && m.Status < 500 {
		h.q.refuse(m.Status)
		return
	}
	rec, ok := h.record(ctx, c, m)
	if !ok {
		h.q.skipped.Add(1)
		return
	}
	h.attachVerdicts(c, m, &rec)
	h.q.enqueue(item{call: &rec})
}

// OnSessionClosed finalises the session once it has gone quiet. The close travels the same queue as
// the calls, so it is applied after every call enqueued before it.
func (h *Hook) OnSessionClosed(_ context.Context, s tollgate.Session) {
	if s.ID == "" || !uuidRe.MatchString(s.TenantID) {
		return
	}
	h.q.enqueue(item{closeWS: s.TenantID, closeID: s.ID, closeAt: h.opt.Now()})
}

// Close stops accepting records, writes everything queued and stops the batcher; it returns when the
// final flush is done or ctx ends.
func (h *Hook) Close(ctx context.Context) error { return h.q.close(ctx) }

// Status reports the queue and its counters for /statusz (it implements contract.StatusSource).
func (h *Hook) Status() map[string]any {
	st := h.q.status()
	st["unreadable_tool_calls"] = h.opt.Writer.UnreadableToolCalls()
	st["unreadable_evidence"] = h.opt.Writer.UnreadableEvidence()
	st["unexpected_55000"] = h.opt.Writer.UnexpectedStateErrors()
	return st
}
