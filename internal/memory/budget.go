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
	at   time.Time
	over bool
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
	e.bud.m[ws] = budgetEntry{at: now, over: over}
	e.bud.mu.Unlock()
	return over
}

// recordUsage stores one model call of memory's own and refreshes the budget verdict.
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
	delete(e.bud.m, ws)
	e.bud.mu.Unlock()
}
