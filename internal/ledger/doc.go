// Package ledger turns every completed proxied call into rows the dashboard reads: one calls row per
// call and an aggregated sessions row per agent run. It is a tollgate Hook, not a MetricSink, because
// After sees the whole neutral Call (session, tenant, tool names) and the Metric together (ADR 0019).
//
// The request path never waits for the database: Before stamps the call (no I/O), After enqueues one
// record on a bounded queue and returns, and a batcher goroutine writes the queue in transactions
// every 250 ms or 500 records. When the queue is full the record is dropped and counted; the counters
// and the queue depth are exposed through Status for /statusz. internal/ledger is, with internal/audit,
// the only writer of the ledger tables (rule H4), through the store.
//
// Privacy: the ledger stores metadata only (ids, model, token counts, tool names, timings). The one
// exception is the first user message preview, kept only when the workspace enabled capture and
// privacy mode is off everywhere.
package ledger
