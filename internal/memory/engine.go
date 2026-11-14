package memory

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

// AttrMemory is the Tenant.Attrs key of the workspace's memory settings (the same string as wsauth.AttrMemory;
// a test in cmd/meterwised holds the two equal).
const AttrMemory = "memory"

// Store is what the memory feature needs from Postgres. *store.Store implements it; tests may fake it.
type Store interface {
	InsertTrace(ctx context.Context, t store.NewTrace) (int64, bool, error)
	InsertTraceFailure(ctx context.Context, f store.TraceFailure) error
	NearestTraces(ctx context.Context, workspaceID string, vec []float32, k int, minConf float32) ([]store.Candidate, error)
	TouchTrace(ctx context.Context, workspaceID string, traceID int64) error
	RecountTraceVotes(ctx context.Context, workspaceID string, traceID int64) (int, error)
	ListTraces(ctx context.Context, workspaceID, cursor string, limit int) (contract.TraceList, error)
	TraceDetail(ctx context.Context, workspaceID, traceID string) (contract.TraceDetail, error)
	ForgetTraces(ctx context.Context, workspaceID string) (int64, error)
	PruneMemory(ctx context.Context, cutoff time.Time) (int64, error)
	AddSelfUsage(ctx context.Context, workspaceID, kind, model string, in, out int64, cost contract.MicroUSD) error
	SelfUsageSince(ctx context.Context, workspaceID string, since time.Time) (contract.MicroUSD, error)
	WorkspaceCaptureAllowed(ctx context.Context, workspaceID string) (bool, error)
	MemoryStats(ctx context.Context, workspaceID string, r contract.SavingsRange, now time.Time) (contract.MemoryStats, error)
	SessionMemoryHits(ctx context.Context, workspaceID, sessionID string) ([]store.HitRow, error)
	MemoryHit(ctx context.Context, workspaceID string, hitID int64) (store.HitRow, error)
	UncreditedHits(ctx context.Context, workspaceID, sessionID string) ([]int64, error)
	SessionBaseline(ctx context.Context, workspaceID, sessionID string, since time.Time) (store.Baseline, error)
}

// HitLedger is how memory reaches the memory_hits writes: the ledger owns that table (H4), so hit feedback and the
// likely credit go through *ledger.MemoryPort.
type HitLedger interface {
	SetHitFeedback(ctx context.Context, workspaceID string, hitID int64, fb *string) (store.HitRef, error)
	CreditMemory(ctx context.Context, workspaceID, sessionID string, hitID, tokens int64, usd contract.MicroUSD) error
}

// Options configures the engine. Store and Ledger are required; without a Distiller and Embedder (no MEMORY_API_KEY)
// the engine only serves the read API and reports no_key.
type Options struct {
	Store     Store
	Ledger    HitLedger
	Distiller Distiller
	Embedder  Embedder

	Model         string            // MEMORY_MODEL, used when a workspace names none
	EmbedModel    string            // MEMORY_EMBED_MODEL
	SelfBudget    contract.MicroUSD // per workspace per UTC day
	RetentionDays int
	PrivacyMode   *bool // the global PRIVACY_MODE; nil means on
	// Price prices a model call of our own. Unpriced models fall back to a flat 1 micro-USD per token, an
	// overestimate that keeps the budget honest.
	Price       func(model string, in, out int64) (contract.MicroUSD, bool)
	MaxSessions int // bound of the per-session state; default 50000
	Now         func() time.Time
	Log         *slog.Logger
}

// Engine is the memory feature: the data-plane Hook, the control-plane API and the background jobs share it.
type Engine struct {
	opt  Options
	ctx  context.Context
	stop context.CancelFunc
	sess *sessions
	jobs *jobs
	bud  *budgets
	cfgs sync.Map // canonical settings JSON -> contract.MemoryConfig

	editFailed, injected, observed, recalls, recallHits, skippedPrivacy, skippedBudget, pruned, noState atomic.Int64
}

// New builds an engine. Call Start to run the pruning loop and Close to stop the jobs.
func New(ctx context.Context, o Options) *Engine {
	if o.MaxSessions <= 0 {
		o.MaxSessions = 50000
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	ctx, stop := context.WithCancel(context.WithoutCancel(ctx))
	e := &Engine{opt: o, ctx: ctx, stop: stop, sess: newSessions(o.MaxSessions), bud: newBudgets()}
	e.jobs = newJobs(ctx, o.Log)
	return e
}

// Keyed reports whether memory has a model key: without one nothing is captured or recalled.
func (e *Engine) Keyed() bool { return e.opt.Distiller != nil && e.opt.Embedder != nil }

// Start runs the pruning loop until Close.
func (e *Engine) Start() {
	e.jobs.wg.Add(1)
	go func() {
		defer e.jobs.wg.Done()
		e.pruneLoop(e.ctx)
	}()
}

// Close stops accepting jobs, cancels the running ones and waits a short while for them.
func (e *Engine) Close() {
	e.stop()
	e.jobs.close()
}

// Hook returns the tollgate hook; nil when the engine has no key (nothing to capture or recall).
func (e *Engine) Hook() tollgate.Hook {
	if !e.Keyed() {
		return nil
	}
	return &Hook{e: e}
}

// Source is the /statusz counters of the engine, also when it has no key.
func (e *Engine) Source() contract.StatusSource { return &Hook{e: e} }

// config reads the workspace's memory settings from the tenant attributes, parsing each distinct JSON once.
func (e *Engine) config(c *tollgate.Call) contract.MemoryConfig {
	attr := c.Tenant.Attrs[AttrMemory]
	if attr == "" {
		return contract.DefaultMemoryConfig()
	}
	if v, ok := e.cfgs.Load(attr); ok {
		return v.(contract.MemoryConfig)
	}
	cfg := contract.ParseMemoryAttr(attr)
	e.cfgs.Store(attr, cfg)
	return cfg
}

// mayCapture is the ledger's capture rule (plan 09-11 P4, ruling A4): the workspace opted in to capture, privacy mode is
// off globally and the workspace did not switch it on. The strictest of the three wins.
func (e *Engine) mayCapture(c *tollgate.Call) bool {
	p := c.Tenant.Policy
	if p == nil || !p.Capture || e.opt.PrivacyMode == nil || *e.opt.PrivacyMode {
		return false
	}
	return p.PrivacyMode == nil || !*p.PrivacyMode
}

func (e *Engine) distilModel(cfg contract.MemoryConfig) string {
	if cfg.Model != "" {
		return cfg.Model
	}
	return e.opt.Model
}

func (e *Engine) embedModel(cfg contract.MemoryConfig) string {
	if cfg.EmbedModel != "" {
		return cfg.EmbedModel
	}
	return e.opt.EmbedModel
}
