package dataplane

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/history"
	"github.com/ethosagent/meterwise/internal/monitors"
	"github.com/ethosagent/meterwise/internal/wire"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// ChainOptions are the settings the history and monitors hooks read from the process config.
type ChainOptions struct {
	PrivacyMode     bool  // PRIVACY_MODE
	HistoryMaxBytes int64 // HISTORY_MAX_BYTES; 0 takes history's default
}

// HookChain returns the hooks serve registers, in the one order that works (plan 06, R2): wsauth
// stamps the tenant, history stamps the ring and the verdict pointer, monitors may short-circuit and
// fill the verdicts, the injected hooks follow, and the ledger comes last: it seals the verdicts in
// After and records. The sources are the stateful hooks for /statusz. serve and cmd/demo-traffic
// both build their chain here so the demo cannot drift from production.
func HookChain(o ChainOptions, injected []tollgate.Hook, ledger tollgate.Hook) ([]tollgate.Hook, []contract.StatusSource) {
	maxSessions := tollgate.DefaultLimits().MaxSessions
	hist := history.New(history.Options{MaxSessions: maxSessions, MaxBytes: o.HistoryMaxBytes})
	mon := monitors.New(monitors.Options{PrivacyMode: o.PrivacyMode, Editor: wire.Editor{}, MaxSessions: maxSessions})
	hooks := append([]tollgate.Hook{wsauth.Hook{}, hist, mon}, injected...)
	return append(hooks, ledger), []contract.StatusSource{hist, mon}
}
