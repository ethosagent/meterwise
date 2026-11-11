package trim

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
)

// AttrTrim is the Tenant.Attrs key wsauth fills with the workspace's canonical trim config
// (contract.TrimConfig.Attr).
const AttrTrim = "trim"

// Region size limits: a result under minRegion bytes is not worth analysing, one over maxRegion is not analysed.
const (
	minRegion = 256
	maxRegion = 1 << 20
)

// Options configures the Hook. The zero value is usable.
type Options struct {
	// MaxSessions bounds the sessions whose decisions are remembered. Default 100000.
	MaxSessions int
	// MaxMemoBytes bounds the bytes those decisions hold across all sessions. Default 64 MiB.
	MaxMemoBytes int64
	// CallBytes is how many bytes of new regions one call may analyse. Default 2 MiB.
	CallBytes int
	// MaxWork is the longest one call may spend analysing new regions. Default 20 ms.
	MaxWork time.Duration
	// MinDeadline is the margin kept before the chain's deadline. Default 5 ms.
	MinDeadline time.Duration
	// Now reads the clock for the deadline only. Default time.Now.
	Now func() time.Time
}

// Hook is the trim tollgate Hook (Name "trim"). It acts in Before only, does no I/O, and keeps one bounded memo of
// region decisions per session.
type Hook struct {
	tollgate.NoopHook
	opt  Options
	pipe *Pipeline
	cfgs *configMemo
	memo *memoStore

	calls, acted, observed, editFailed, deadlineSkips, unfolds atomic.Int64
}

// New builds a Hook.
func New(o Options) *Hook {
	if o.MaxSessions <= 0 {
		o.MaxSessions = 100000
	}
	if o.MaxMemoBytes <= 0 {
		o.MaxMemoBytes = 64 << 20
	}
	if o.CallBytes <= 0 {
		o.CallBytes = 2 << 20
	}
	if o.MaxWork <= 0 {
		o.MaxWork = 20 * time.Millisecond
	}
	if o.MinDeadline <= 0 {
		o.MinDeadline = 5 * time.Millisecond
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Hook{opt: o, pipe: NewPipeline(), cfgs: newConfigMemo(), memo: newMemoStore(o.MaxSessions, o.MaxMemoBytes)}
}

// Name identifies the hook.
func (*Hook) Name() string { return "trim" }

func sessKey(tenant, session string) string { return tenant + "\x00" + session }

// Before decides, per tool-result region, what the request carries and splices that in. It never short-circuits.
// With the workspace in observe mode it leaves the body byte-identical and reports what it would have saved. A wire
// error leaves the body untouched and is counted. The result is reported in Meta[contract.MetaTrim].
func (h *Hook) Before(ctx context.Context, c *tollgate.Call) (*tollgate.Response, error) {
	w := c.Provider.Wire
	if (w != tollgate.WireOpenAIChat && w != tollgate.WireAnthropicMessages) || len(c.Body) == 0 || c.Session.ID == "" {
		return nil, nil
	}
	cfg := h.cfgs.get(c.Tenant.Attrs[AttrTrim])
	if cfg.cfg.Mode == contract.TrimModeOff {
		return nil, nil
	}
	h.calls.Add(1)
	if ctx.Err() != nil {
		h.deadlineSkips.Add(1)
		return nil, nil
	}
	run := h.newRun(ctx, c, cfg)
	run.execute()
	return nil, nil
}

// OnSessionClosed forgets the session's decisions.
func (h *Hook) OnSessionClosed(_ context.Context, s tollgate.Session) {
	h.memo.remove(sessKey(s.TenantID, s.ID))
}

// Status is the hook's counters for /statusz.
func (h *Hook) Status() map[string]any {
	return map[string]any{
		"calls": h.calls.Load(), "acted": h.acted.Load(), "observed": h.observed.Load(), "edit_failed": h.editFailed.Load(),
		"deadline_skips": h.deadlineSkips.Load(), "unfolds": h.unfolds.Load(), "memo_evictions": h.memo.evicted.Load(),
		"memo_sessions": h.memo.sessions(), "memo_bytes": h.memo.total.Load(),
	}
}

// cfgEntry is a decoded config and the pipeline settings derived from it.
type cfgEntry struct {
	cfg contract.TrimConfig
	st  Settings
}

func settingsOf(cfg contract.TrimConfig) Settings {
	en := make(map[contract.TrimStage]bool, 5)
	for _, s := range contract.TrimStages() {
		en[s] = cfg.Stages[s]
	}
	en[contract.StageSchemaTrim] = en[contract.StageSchemaTrim] && cfg.Mode == contract.TrimModeAggressive
	return Settings{MinGainPct: cfg.MinGainPct, MaxFoldPct: cfg.MaxFoldPct, Enabled: en}
}

func (e cfgEntry) never(tool string) bool {
	for _, n := range e.cfg.NeverTrimTools {
		if strings.EqualFold(n, tool) {
			return true
		}
	}
	return false
}

const configMemoCap = 1024

// configMemo decodes the tenant attr once per distinct string, so Before does no per-call parse.
type configMemo struct {
	mu       sync.RWMutex
	m        map[string]cfgEntry
	defaults cfgEntry
}

func newConfigMemo() *configMemo {
	d := contract.DefaultTrimConfig()
	return &configMemo{m: map[string]cfgEntry{}, defaults: cfgEntry{d, settingsOf(d)}}
}

func (c *configMemo) get(attr string) cfgEntry {
	if attr == "" {
		return c.defaults
	}
	c.mu.RLock()
	e, ok := c.m[attr]
	c.mu.RUnlock()
	if ok {
		return e
	}
	cfg := contract.ParseTrimAttr(attr)
	e = cfgEntry{cfg, settingsOf(cfg)}
	c.mu.Lock()
	if len(c.m) >= configMemoCap {
		clear(c.m)
	}
	c.m[attr] = e
	c.mu.Unlock()
	return e
}
