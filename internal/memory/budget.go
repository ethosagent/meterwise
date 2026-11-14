package memory

import (
	"context"
	"sync"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// budgetTTL is how long a workspace's over-budget verdict is reused before the database is asked again.
const budgetTTL = 30 * time.Second

type budgets struct {
	mu sync.Mutex
	m  map[string]budgetEntry
}

type budgetEntry struct {
	at    time.Time
	over  bool
	spent contract.MicroUSD
}

func newBudgets() *budgets { return &budgets{m: map[string]budgetEntry{}} }

// overBudget reports whether memory's own spend of the workspace today (UTC) has reached the daily budget. Memory's
// jobs pause while it has; the settings and /statusz say so.
func (e *Engine) overBudget(ctx context.Context, ws string) bool {
	now := e.opt.Now()
	e.bud.mu.Lock()
	b, ok := e.bud.m[ws]
	e.bud.mu.Unlock()
	if ok && now.Sub(b.at) < budgetTTL {
		return b.over
	}
	day := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	spent, err := e.opt.Store.SelfUsageSince(ctx, ws, day)
	if err != nil {
		return false // a failed read must not stop memory; the next call asks again
	}
	over := e.opt.SelfBudget > 0 && spent >= e.opt.SelfBudget
	e.bud.mu.Lock()
	if len(e.bud.m) > 4096 {
		e.bud.m = map[string]budgetEntry{}
	}
	e.bud.m[ws] = budgetEntry{at: now, over: over, spent: spent}
	e.bud.mu.Unlock()
	return over
}

// budgetState is what /statusz shows of the daily budget: the cap per workspace, and, over the workspaces read in the
// last day, how many are paused and what they spent (UTC day of the read; no workspace ids).
func (e *Engine) budgetState() (capMicro contract.MicroUSD, paused int, spent contract.MicroUSD) {
	now := e.opt.Now()
	e.bud.mu.Lock()
	defer e.bud.mu.Unlock()
	for _, b := range e.bud.m {
		if now.Sub(b.at) > 24*time.Hour {
			continue
		}
		spent += b.spent
		if b.over {
			paused++
		}
	}
	return e.opt.SelfBudget, paused, spent
}

// recordUsage stores one model call of memory's own and adds its cost to the cached budget verdict in place, so the
// verdict stays fresh and keeps its TTL.
func (e *Engine) recordUsage(ctx context.Context, ws, kind, model string, u Usage) {
	if u.Input == 0 && u.Output == 0 {
		return
	}
	cost := contract.MicroUSD(u.Input + u.Output) // flat 1 micro-USD per token unless the model is priced
	if e.opt.Price != nil {
		if v, ok := e.opt.Price(model, u.Input, u.Output); ok {
			cost = v
		}
	}
	if err := e.opt.Store.AddSelfUsage(ctx, ws, kind, model, u.Input, u.Output, cost); err != nil {
		e.opt.Log.Debug("memory usage not recorded", "err", err.Error())
	}
	e.bud.mu.Lock()
	if b, ok := e.bud.m[ws]; ok {
		b.spent += cost
		b.over = e.opt.SelfBudget > 0 && b.spent >= e.opt.SelfBudget
		e.bud.m[ws] = b
	}
	e.bud.mu.Unlock()
}
