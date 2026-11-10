# 0019. The ledger write path: Hook.After, a bounded queue, a batcher, idempotent rows

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** Plan 05 sections 3 and 6; refines ADR 0004

## Context

ADR 0004 put the ledger in `Hook.After`, not a `MetricSink`: `After(ctx, *Call, Metric)` sees the
session, tenant and neutral events together, while `Metric` lacks tool calls and session facts.
Writing a row per call from `After` would put Postgres latency on every request path, so the write
must be decoupled. Options: a synchronous insert (blocks, fails the request on a slow database), an
unbounded channel (memory without limit), a bounded queue with a batcher (chosen).

## Decision

`ledger.Hook.After` builds one record and enqueues it on a bounded queue (10 000) without blocking; a
full or closed queue drops the record and counts it. One batcher goroutine writes the queue in one
transaction every 250 ms or 500 records: `INSERT calls ON CONFLICT (id) DO NOTHING`, then one
`sessions` upsert per session for the rows actually inserted, then one coalesced
`workspaces.last_call_at` touch per workspace. A failed batch is retried with backoff and, if it keeps
failing for a reason other than an unreachable database, retried record by record so one refused record
cannot sink the rest. `OnSessionClosed` travels the same queue, so a close is applied after the calls
enqueued before it. `Close(ctx)` flushes. Tool-call names and text presence come from `OnEvent`
(neutral events), never from provider JSON; a non-streamed response has no events, so its tool names
stay empty in tollgate v0.1.0. Sessions are keyed `(workspace_id, id)`. Session closes use the same retry path as calls and a close that still fails is counted
in `lost_closes`. `Options.PrivacyMode` is a `*bool` and nil means privacy ON. A record whose `org_id` is
not `store.OrgID()` is refused and counted in `skipped_wrong_org`. Status (queue depth, dropped,
flushed, lost, lost_closes, unpriced, last error) is exposed through `Status()` for `/statusz`.

## Consequences

Easier: the request path adds about 2 microseconds (hook benchmark) and its p99 is unchanged within
noise (`TestAddedLatencyP99UnderBudgetWith*`); replaying a batch changes nothing.
Harder: a crash loses up to one flush interval of rows, and a database outage longer than the queue
(10 000 calls) drops records, visibly. Not a billing system.
Forbidden: any other writer of `calls`, `sessions`, `monitor_events` (archtest H4).
Enforced by: `TestQueueNeverBlocksAndDropsAreCounted`, `TestWriteCallsIsIdempotentAndAggregatesSessions`,
`TestFailedWritesRetryThenIsolateAPoisonRecord`, `TestNoMessageTextIsPersistedUnlessCaptureOnAndPrivacyOff`.

Deviation: plan 05 section 3 said `make demo-traffic` replays tollgate testkit fixtures. `cmd/demo-traffic`
cannot import the testkit (archtest H1/H4: it is test-only), so it serves hand-made request and response
bodies shaped like the fixtures from its own loopback upstream. The scenarios therefore do not track fixture changes.

Fix round 1: a call refused before the upstream (status under 500, finish `rejected`: admission 429, provider
or CIDR 403, unknown or retired id 404/410) is not recorded; `/statusz` counts it in `refused_by_status`. A failed
batch is retried with doubling backoff (200 ms to 5 s) for up to 30 s while the database is unreachable, bounded
by the queue size. The cost model is the one tollgate reports on the Metric, else the request's; v0.1.0 reports the
request's, which the client controls (fine for metering, not for billing).
