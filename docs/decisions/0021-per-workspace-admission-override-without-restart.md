# 0021. Per-workspace admission override without a restart

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/05-proxy-data-plane.md §3 (Admission), 05-interface.md

## Context

Each workspace is capped at `MAX_CONCURRENT_PER_WORKSPACE` in-flight calls (default 8), and an admin
may set `settings.maxConcurrent` for one workspace. tollgate's `ext/admission/concurrency.Limiter`
fixes a tenant's cap when its bucket is created and takes per-tenant overrides only in
`concurrency.Config.PerTenant` at construction. Changing a setting must not need a restart.

## Decision

`dataplane.Admission` keeps one `Limiter` per distinct cap value, created on first use (the default,
plus at most `wsauth.MaxConcurrentCap` = 1024 others). The override travels on the tenant:
`wsauth.Resolve` reads `settings.maxConcurrent` once when the directory cache is filled (whole
number, at least 1, clamped to 1024; anything else means "use the default") and puts it in
`Tenant.Attrs["max_concurrent"]`. The directory busts the entry on every settings update, so the
next call re-reads it. `Acquire` picks the limiter for the tenant's current cap; the limiter keys
its buckets by tenant id, so workspaces stay isolated. Queue timeout is 5 s, then `429` with
`Retry-After: 2`.

Alternatives: rebuild one limiter on every change (loses in-flight accounting, races with
releases); a hand-written semaphore per workspace (duplicates a tested library); an unbounded map of
limiters keyed by workspace (grows with tenants for no benefit).

## Consequences

Easier: no restart, no new dependency, hot path is an `Atoi` and a map read under a read lock.
Harder: when the cap changes while calls are in flight, calls admitted under the old cap finish on
the old limiter, so for their duration the workspace can hold old plus new slots at once (bounded by
the sum of the two caps; nothing leaks). The cache TTL is 30 s but a settings update busts at once,
so the delay is the next request. Enforced by
`TestTheWorkspaceOverrideWinsAndChangesApplyWithoutARestart`. Related: the authenticator's cache-hit
path now takes no deadline (the miss path keeps the 250 ms bound of ADR 0017).
