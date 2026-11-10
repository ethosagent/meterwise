package monitors

import (
	"sync/atomic"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Names of the hook's counters on /statusz; fires_<monitor> is added per monitor.
const (
	ctrSkipped         = "skipped_deadline"
	ctrNotesNotAdopted = "notes_not_adopted"
	ctrBlocksDegraded  = "blocks_degraded"
	ctrBlocksCapped    = "blocks_capped"
	ctrBlocks          = "blocks"
	ctrNotesInjected   = "notes_injected"
	ctrNotesDropped    = "notes_dropped"
	ctrUnfoldSkips     = "unfold_skips"
)

// counters is the one table of the hook's counters: Status reports every entry, and the hook keeps the
// pointer of each one it bumps, so the hot paths never look a name up.
type counters struct{ m map[string]*atomic.Int64 }

func newCounters() *counters {
	c := &counters{m: map[string]*atomic.Int64{}}
	for _, n := range []string{ctrSkipped, ctrNotesNotAdopted, ctrBlocksDegraded, ctrBlocksCapped, ctrBlocks, ctrNotesInjected, ctrNotesDropped, ctrUnfoldSkips} {
		c.m[n] = new(atomic.Int64)
	}
	for _, n := range contract.MonitorNames() {
		c.m[firesName(n)] = new(atomic.Int64)
	}
	return c
}

func firesName(m contract.MonitorName) string { return "fires_" + string(m) }

func (c *counters) of(name string) *atomic.Int64 { return c.m[name] }

// fill adds every counter to m.
func (c *counters) fill(m map[string]any) {
	for n, v := range c.m {
		m[n] = v.Load()
	}
}
