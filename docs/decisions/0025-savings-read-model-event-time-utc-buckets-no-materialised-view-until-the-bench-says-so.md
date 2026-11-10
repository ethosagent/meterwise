# 0025. Savings read model: event-time UTC buckets, no materialised view until the bench says so

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/07-savings-dashboard.md §4, 07-interface.md rulings S2, S4, S7 and S8

## Context

The savings dashboard needs a hero, a per-day chart, per-monitor causes and a per-client table over 7d, 30d
or a custom range. Money lives in two places: `monitor_events` (one row per verdict, with `est_saved_usd`) and
`calls.cost_usd`. `sessions.saved_usd` is a running sum that moves when the session's calls land, so
bucketing a chart by session start would put a session that crosses midnight on the wrong day. Options:
bucket by session start (cheap, wrong day), bucket by event time (right day, an aggregate per read), or
keep a `savings_daily` materialised view (fast, but a migration, a refresh job and a staleness window).

## Decision

Savings are read live from `monitor_events` by `ts` and `calls` by `started_at`, bucketed per UTC day
(`(ts AT TIME ZONE 'UTC')::date`), with totals from `GROUPING SETS` as `Stats` does. Saved is acted and
certain, likely is acted and likely, would-save is observe: the filters of `MonitorStats`, so the hero equals
`SUM(sessions.saved_usd)` for sessions inside the range. One read runs in one REPEATABLE READ snapshot so the
hero, the days and the causes always agree. Days are UTC only. No materialised view is added: the bench
(`TestSavingsReadsStayUnderBudgetAt50kCalls`: 50k calls, 30 days, 5k sessions, 5k events) must stay under
p95 300 ms. On a miss the work stops and a view goes through a migration and a new ADR.

Client attribution stays in the existing `calls.client` and `sessions.client` columns. `ledger.ClientFamily`
reads only the User-Agent product token (no version, lower-case, at most 64 bytes); old rows keep their raw
token and the UI groups them as stored. Only User-Agent strings present in committed fixtures are named in
tests; none is guessed.

## Consequences

Easier: one source of money, every figure traces to event rows, no refresh lag. Harder: every read is an
aggregate over the range (measured p95 on the bench rig: overview about 100 ms, causes about 170 ms, saves
page one about 15 ms, session detail about 7 ms, with `-race`); a workspace far past 50k calls per 30 days
will need the view. A per-user timezone is deferred (it needs a preferences field and its own ADR).
Enforced by `TestOverviewHeroDaysAndCauses`, `TestSavesPagingIsStableAndScoped` (internal/store),
`TestSavingsPayloads` (internal/api, which walks the JSON for a saved-plus-likely money field) and the bench.
