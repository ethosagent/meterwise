# Streaming (`STR-*`)

Guards non-negotiables 1 and 6 in `CLAUDE.md`. tollgate (the library) owns the
byte path; this lens covers what the host adds around it: hooks, the
authenticator and admission. Read tollgate's ADR 0004 for the stream contract.

## Already covered by CI

- `go test -race` (data races, not ordering or latency).
- archtest H3: `history`, `monitors`, `savings` cannot import `net/http`, `database/sql` or `store`.

## Checks

**STR-1 Hooks never block the client.** `Before`, `OnEvent` and `After` return fast;
anything slow (database, network) is handed to a bounded queue and done elsewhere.

**STR-2 No I/O in `Before`.** `Before` reads only in-memory state (the history ring,
the settings snapshot). A database read in `Before` is a finding.

**STR-3 Bounded everything.** Each channel, ring and queue has a size from config;
overflow drops the oldest or newest and increments a counter. Never grow without limit.

**STR-4 A broken hook degrades to pass-through.** Panics are recovered by tollgate's
chain; the host's code must not rely on that to be correct, and must not swallow
errors that should surface in a metric.

**STR-5 No body buffering.** The host never reads a request or response body in full
to inspect it; it uses the neutral `Call` and `Event` that tollgate provides.

**STR-6 Ledger writes leave the request.** `Hook.After` enqueues; a background writer
batches to Postgres. A slow database never slows or fails a proxied call.

**STR-7 Cancellation is respected.** Background writers stop on shutdown with a
drain deadline; a client disconnect is recorded with the right finish reason.

**STR-8 Goroutines end.** Every goroutine the host starts has an owner that stops it;
tests check for leaks.
