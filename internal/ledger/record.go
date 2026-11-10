package ledger

import (
	"cmp"
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

const (
	maxToolNames = 32
	maxNameLen   = 128
	maxFieldLen  = 200
)

// PreviewMax is the longest first-user-message preview, in characters.
const PreviewMax = 200

// record builds the calls row from the neutral Call and Metric. It reports false for a call that
// cannot be attributed to a workspace (no tenant uuid or org: the dev NoAuth tenant).
func (h *Hook) record(ctx context.Context, c *tollgate.Call, m tollgate.Metric) (contract.CallRecord, bool) {
	ws, org := c.Tenant.ID, c.Tenant.Attrs["org_id"]
	if !tenantOK(c.Tenant) || m.CallID == "" {
		return contract.CallRecord{}, false
	}
	st, _ := c.Meta[metaState].(*callState)
	if st == nil {
		st = &callState{}
	}
	start := h.startOf(st, m)
	idx := st.index
	if idx < 1 {
		idx = max(c.Session.Calls, 1)
	}
	st.mu.Lock()
	tools := append([]string(nil), st.tools...)
	st.mu.Unlock()
	u := contract.Usage{Input: m.Usage.InputTokens, Output: m.Usage.OutputTokens, Cached: m.Usage.CachedInputTokens, Reasoning: m.Usage.ReasoningTokens}
	// The model tollgate reports on the Metric wins (the response model, once tollgate carries it; in
	// v0.1.0 it is the request's); the request's model is the fallback. Both come from the neutral Call.
	provider, model := clean(m.Provider, maxFieldLen), clean(cmp.Or(m.Model, c.Model), maxFieldLen)
	var cost *contract.MicroUSD
	if v, ok := h.opt.Pricer.Cost(provider, model, u, start); ok {
		cost = &v
	} else {
		h.q.unpriced.Add(1)
		h.reportUnpriced(ctx, provider, model)
	}
	sid, src, sstart := sessionOf(c, m, start)
	return contract.CallRecord{
		ID: clean(m.CallID, maxFieldLen), OrgID: org, WorkspaceID: ws, SessionID: clean(sid, maxFieldLen), CallIndex: idx, StartedAt: start.UTC(),
		Provider: provider, Model: model, Wire: clean(string(c.Provider.Wire), maxFieldLen), Stream: m.Stream, Status: m.Status,
		Finish: clean(string(m.Finish), maxFieldLen), Usage: nonNeg(u), Cost: cost, TTFBMs: m.TTFB.Milliseconds(), DurationMs: m.Duration.Milliseconds(),
		ToolCalls: tools, Client: clientOf(c), SessionSource: src, SessionStart: sstart.UTC(), LastHadText: hadText(st, m, u, tools), Preview: st.preview,
	}, true
}

// startOf is when the call began: the Metric's start, else when it reached the hook, else now.
func (h *Hook) startOf(st *callState, m tollgate.Metric) time.Time {
	switch {
	case !m.Start.IsZero():
		return m.Start
	case !st.start.IsZero():
		return st.start
	}
	return h.opt.Now()
}

// sessionOf is the session id, how it was learned and when it started; a call without a session id gets a
// predicted one of its own.
func sessionOf(c *tollgate.Call, m tollgate.Metric, start time.Time) (id, source string, started time.Time) {
	id, source = c.Session.ID, contract.SessionSourcePredicted
	if c.Session.Source == tollgate.SessionFromHeader {
		source = contract.SessionSourceHeader
	}
	if id == "" {
		id, source = "call-"+m.CallID, contract.SessionSourcePredicted
	}
	if started = c.Session.StartedAt; started.IsZero() {
		started = start
	}
	return id, source, started
}

// hadText: the assistant produced text. A non-streamed response has no text events (the hook never reads
// provider JSON), so a call that finished ok with output tokens and no tool call is taken to have answered in text.
func hadText(st *callState, m tollgate.Metric, u contract.Usage, tools []string) bool {
	return st.text.Load() || (!m.Stream && m.Finish == tollgate.FinishOK && u.Output > 0 && len(tools) == 0)
}

func nonNeg(u contract.Usage) contract.Usage {
	return contract.Usage{Input: max(u.Input, 0), Output: max(u.Output, 0), Cached: max(u.Cached, 0), Reasoning: max(u.Reasoning, 0)}
}

// reportUnpriced logs each unpriced model once (bounded), so a missing price is visible, not silent.
func (h *Hook) reportUnpriced(ctx context.Context, provider, model string) {
	if h.nwrn.Load() >= 256 {
		return
	}
	if _, seen := h.warn.LoadOrStore(provider+"/"+model, true); !seen {
		h.nwrn.Add(1)
		h.opt.Log.LogAttrs(ctx, slog.LevelWarn, "ledger: no price for model; call cost is NULL",
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
