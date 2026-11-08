# 0004. The ledger is written from Hook.After

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 00 D8

## Context

tollgate's `Metric` lacks tool calls, cost and the blocked flag; `After(ctx, *Call, Metric)` has everything. Options: a `MetricSink` (chosen against), `Hook.After` (chosen).

## Decision

`ledger` implements `Hook.After`/`OnSessionClosed` and is the only writer of `calls`, `sessions`, `monitor_events`. `Options.Metrics` stays a discard or stdout sink.

## Consequences

One writer, one idempotency rule (`call_id`). `After` only enqueues; a batching writer owns Postgres. Enforced by: archtest H4 (grep), `DATA-1`, `STR-6`.
