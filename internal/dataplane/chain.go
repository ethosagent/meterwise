package dataplane

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
	"github.com/ethosagent/meterwise/internal/monitors"
	"github.com/ethosagent/meterwise/internal/wire"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// ChainOptions are the settings the hooks read from the process config.
type ChainOptions struct {
	PrivacyMode     bool                  // PRIVACY_MODE
	HistoryMaxBytes int64                 // HISTORY_MAX_BYTES; 0 takes history's default
	Router          contract.RouterTables // pairs, capabilities and priced models for the router hook; zero in step 0
	Memory          tollgate.Hook         // the memory hook, built by the caller; nil when memory is not configured
}

// chainSlots are the hooks HookChain places by name. A nil slot is skipped.
type chainSlots struct {
	wsauth, history, trim, memory, router, monitors tollgate.Hook
}

// order lays the slots out in the one order that works (ADR 0026), the injected hooks after monitors and
// the ledger last, and returns the slots that report their own counters for /statusz.
func (s chainSlots) order(injected []tollgate.Hook, ledger tollgate.Hook) ([]tollgate.Hook, []contract.StatusSource) {
	var hooks []tollgate.Hook
	var srcs []contract.StatusSource
	for _, h := range []tollgate.Hook{s.wsauth, s.history, s.trim, s.memory, s.router, s.monitors} {
		if h == nil {
			continue
		}
		hooks = append(hooks, h)
		if src, ok := h.(contract.StatusSource); ok {
			srcs = append(srcs, src)
		}
	}
	return append(append(hooks, injected...), ledger), srcs
}

// HookChain returns the hooks serve registers, in the one order that works (plan 06 R2, plan 09-11 R4):
// wsauth stamps the tenant, history stamps the ring and the verdict pointer, trim and memory edit the
// body, the router picks the model, monitors may short-circuit and fill the verdicts, the injected hooks
// follow, and the ledger comes last: it seals the verdicts in After and records. serve and
// cmd/demo-traffic both build their chain here so the demo cannot drift from production.
func HookChain(o ChainOptions, injected []tollgate.Hook, ledger tollgate.Hook) ([]tollgate.Hook, []contract.StatusSource) {
	maxSessions := tollgate.DefaultLimits().MaxSessions
	s := chainSlots{
		wsauth:   wsauth.Hook{},
		history:  history.New(history.Options{MaxSessions: maxSessions, MaxBytes: o.HistoryMaxBytes}),
		trim:     trimHook(o),
		memory:   o.Memory,
		router:   routerHook(o),
		monitors: monitors.New(monitors.Options{PrivacyMode: o.PrivacyMode, Editor: wire.Editor{}, MaxSessions: maxSessions}),
	}
	return s.order(injected, ledger)
}
