// Package savings holds the pure money math of the hot path: pricing token usage from an in-memory
// snapshot and, from Plan 06, estimating what a monitor saved. It performs no I/O (rule H3): the
// store-backed loader that fills the snapshot lives in internal/ledger (ruling R11).
package savings
