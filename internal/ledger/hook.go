package ledger

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Meta keys the hook stamps on a call (scratch space shared by extensions for that call).
const (
	MetaStart = "ledger.start" // time.Time the call reached the hook
	MetaIndex = "ledger.index" // int, the call's position in its session (1-based)
	metaState = "ledger.state"
)

// PreviewMax is the longest first-user-message preview, in characters.
const PreviewMax = 200

const (
	maxToolNames = 32
	maxNameLen   = 128
	maxFieldLen  = 200
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Hook is the ledger's tollgate Hook (Name "ledger"). Create it with New; it owns a batcher
// goroutine that Close stops after a final flush.
type Hook struct {
	tollgate.NoopHook
	opt  Options
	pr   contract.Pricer
	q    *queue
	warn sync.Map // models already reported as unpriced
	nwrn atomic.Int64
}

// callState is what OnEvent learns about one streamed call; After reads it.
type callState struct {
	mu      sync.Mutex
	tools   []string
	text    bool
	preview string
}

// New builds the hook and starts its batcher. Pricer and Writer are required.
func New(o Options) (*Hook, error) {
	o, err := o.withDefaults()
	if err != nil {
		return nil, err
	}
	h := &Hook{opt: o, pr: o.Pricer}
	h.q = newQueue(o)
	h.q.start()
	return h, nil
}

// Name identifies the hook.
func (*Hook) Name() string { return "ledger" }

// Before stamps the call and, only when capture is on, remembers the first user message preview. It
// does no I/O and no allocation beyond the state record.
func (h *Hook) Before(_ context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	if c.Meta == nil {
		c.Meta = map[string]any{}
	}
	st := &callState{}
	if c.Session.Calls <= 1 && h.mayCapture(c) {
		st.preview = previewOf(c.Messages)
	}
	c.Meta[MetaStart] = h.opt.Now()
	c.Meta[MetaIndex] = c.Session.Calls
	c.Meta[metaState] = st
	return nil, nil
}

// mayCapture: the workspace opted in to capture, privacy mode is off globally, and the workspace
// did not switch it on for itself. The strictest of the three wins.
func (h *Hook) mayCapture(c *tollgate.Call) bool {
	p := c.Tenant.Policy
	if p == nil || !p.Capture || h.opt.PrivacyMode {
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
		if strings.TrimSpace(e.Text) != "" {
			st.mu.Lock()
			st.text = true
			st.mu.Unlock()
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
func (h *Hook) After(_ context.Context, c *tollgate.Call, m tollgate.Metric) {
	rec, ok := h.record(c, m)
	if !ok {
		h.q.skipped.Add(1)
		return
	}
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
func (h *Hook) Status() map[string]any { return h.q.status() }

// record builds the calls row from the neutral Call and Metric. It reports false for a call that
// cannot be attributed to a workspace (no tenant uuid or org: the dev NoAuth tenant).
func (h *Hook) record(c *tollgate.Call, m tollgate.Metric) (contract.CallRecord, bool) {
	ws, org := c.Tenant.ID, c.Tenant.Attrs["org_id"]
	if !uuidRe.MatchString(ws) || !uuidRe.MatchString(org) || m.CallID == "" {
		return contract.CallRecord{}, false
	}
	start := m.Start
	if start.IsZero() {
		if t, ok := c.Meta[MetaStart].(time.Time); ok {
			start = t
		} else {
			start = h.opt.Now()
		}
	}
	idx, _ := c.Meta[MetaIndex].(int)
	if idx < 1 {
		idx = max(c.Session.Calls, 1)
	}
	st, _ := c.Meta[metaState].(*callState)
	var tools []string
	var sawText bool
	var preview string
	if st != nil {
		st.mu.Lock()
		tools, sawText, preview = append([]string(nil), st.tools...), st.text, st.preview
		st.mu.Unlock()
	}
	u := contract.Usage{Input: m.Usage.InputTokens, Output: m.Usage.OutputTokens, Cached: m.Usage.CachedInputTokens, Reasoning: m.Usage.ReasoningTokens}
	provider, model := clean(m.Provider, maxFieldLen), clean(m.Model, maxFieldLen)
	var cost *contract.MicroUSD
	if v, ok := h.pr.Cost(provider, model, u, start); ok {
		cost = &v
	} else {
		h.q.unpriced.Add(1)
		h.reportUnpriced(provider, model)
	}
	sid, src := c.Session.ID, contract.SessionSourcePredicted
	if c.Session.Source == tollgate.SessionFromHeader {
		src = contract.SessionSourceHeader
	}
	if sid == "" {
		sid, src = "call-"+m.CallID, contract.SessionSourcePredicted
	}
	sstart := c.Session.StartedAt
	if sstart.IsZero() {
		sstart = start
	}
	// A non-streamed response has no text events (the hook never reads provider JSON): a call that
	// finished ok with output tokens and no tool call is taken to have answered in text.
	text := sawText || (!m.Stream && m.Finish == tollgate.FinishOK && u.Output > 0 && len(tools) == 0)
	return contract.CallRecord{
		ID: clean(m.CallID, maxFieldLen), OrgID: org, WorkspaceID: ws, SessionID: clean(sid, maxFieldLen), CallIndex: idx, StartedAt: start.UTC(),
		Provider: provider, Model: model, Wire: clean(string(c.Provider.Wire), maxFieldLen), Stream: m.Stream, Status: m.Status,
		Finish: clean(string(m.Finish), maxFieldLen), Usage: nonNeg(u), Cost: cost, TTFBMs: m.TTFB.Milliseconds(), DurationMs: m.Duration.Milliseconds(),
		ToolCalls: tools, Client: clientOf(c), SessionSource: src, SessionStart: sstart.UTC(), LastHadText: text, Preview: preview,
	}, true
}

func nonNeg(u contract.Usage) contract.Usage {
	return contract.Usage{Input: max(u.Input, 0), Output: max(u.Output, 0), Cached: max(u.Cached, 0), Reasoning: max(u.Reasoning, 0)}
}

// reportUnpriced logs each unpriced model once (bounded), so a missing price is visible, not silent.
func (h *Hook) reportUnpriced(provider, model string) {
	if h.nwrn.Load() >= 256 {
		return
	}
	if _, seen := h.warn.LoadOrStore(provider+"/"+model, true); !seen {
		h.nwrn.Add(1)
		h.opt.Log.LogAttrs(context.Background(), slog.LevelWarn, "ledger: no price for model; call cost is NULL",
			slog.String("provider", provider), slog.String("model", model))
	}
}

// clientOf is the product token of the User-Agent (for example "opencode/1.2"), capped; no credential
// header is ever read.
func clientOf(c *tollgate.Call) string {
	ua := c.Header.Get("User-Agent")
	if ua == "" {
		return ""
	}
	tok, _, _ := strings.Cut(ua, " ")
	return clean(tok, 64)
}

// previewOf is the first user message, trimmed and capped at PreviewMax characters.
func previewOf(msgs []tollgate.Message) string {
	for _, m := range msgs {
		if m.Role == tollgate.RoleUser && strings.TrimSpace(m.Text) != "" {
			return truncRunes(clean(strings.TrimSpace(m.Text), 4*PreviewMax), PreviewMax)
		}
	}
	return ""
}

// clean makes s safe for a text column (valid UTF-8, no NUL) and at most n bytes.
func clean(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if strings.ContainsRune(s, 0) {
		s = strings.ReplaceAll(s, "\x00", "")
	}
	if len(s) > n {
		s = s[:n]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
	}
	return s
}

func truncRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}
