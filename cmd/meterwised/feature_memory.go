package main

import (
	"context"
	"time"

	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/ledger"
	"github.com/ethosagent/meterwise/internal/memory"
)

// memoryFeature builds the trace library: the hook for the data-plane chain (nil without MEMORY_API_KEY, so nothing is
// captured or recalled), the control-plane port (always, so traces can be listed and forgotten and the settings say
// no_key) and the status source for /statusz. The ledger tells it which sessions closed, and hit feedback and the likely
// credit go back through the ledger's port (H4). The engine stops with the pricing loop, ahead of the ledger's flush.
func memoryFeature(ctx context.Context, rt *instance, pricer contract.Pricer) (tollgate.Hook, api.MemoryAPI, contract.StatusSource, error) {
	m := rt.cfg.Memory
	o := memory.Options{
		Store: rt.db, Ledger: ledger.NewMemoryPort(rt.db), Model: m.Model, EmbedModel: m.EmbedModel, SelfBudget: m.SelfBudget,
		RetentionDays: m.RetentionDays, PrivacyMode: &rt.cfg.PrivacyMode, Log: rt.log,
		Price: func(model string, in, out int64) (contract.MicroUSD, bool) {
			return pricer.Cost("openai", model, contract.Usage{Input: in, Output: out}, time.Now())
		},
	}
	if m.Enabled() {
		o.Distiller = memory.NewDistiller(m.BaseURL, m.APIKey)
		o.Embedder = memory.NewEmbedder(m.BaseURL, m.APIKey)
	} else {
		rt.log.Info("memory is off: MEMORY_API_KEY is not set")
	}
	eng := memory.New(ctx, o)
	eng.Start()
	if rt.ledger != nil {
		rt.ledger.SetOnClosed(func(s ledger.ClosedSession) { eng.OnClosed(ctx, s.WorkspaceID, s.SessionID, s.Outcome, s.Calls) })
	}
	prev := rt.stopPricing
	rt.stopPricing = func() {
		if prev != nil {
			prev()
		}
		eng.Close()
	}
	return eng.Hook(), eng.API(), eng.Source(), nil
}
