package dataplane

import (
	"github.com/ethosagent/tollgate"

	"github.com/ethosagent/meterwise/internal/trim"
)

// trimHook builds the trim hook for HookChain's trim slot (after history, before memory, router and monitors). Each
// workspace's mode comes from Tenant.Attrs["trim"] (default observe), so a fresh workspace only measures. The memo of
// region decisions is bounded by the same session limit the other per-session hooks use.
func trimHook(ChainOptions) tollgate.Hook {
	return trim.New(trim.Options{MaxSessions: tollgate.DefaultLimits().MaxSessions})
}
