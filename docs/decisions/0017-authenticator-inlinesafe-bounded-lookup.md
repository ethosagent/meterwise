# 0017. The workspace authenticator is InlineSafe, with a bounded miss

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/04-workspace-url-setup-and-audit-log.md, 04-interface.md ("InlineSafe only on a cache hit")

## Context

tollgate v0.1.0 runs an `Authenticator` under a deadline goroutine unless its type declares
`InlineSafe` (its own ADR 0015): then it runs on the handler goroutine with no deadline. The marker
is a type-level method (promotion from an embedded type does not count) so it cannot be switched
per call. The brief asked for it "only on a cache hit". `Authenticate` is a map read on a hit but
reads Postgres on a miss (30 s cache, so one miss per workspace per 30 s per process).

## Decision

`wsauth.Authenticator` declares `InlineSafe` and makes the claim true by bounding the one slow
path: the directory lookup runs under `context.WithTimeout(ctx, 250ms)` and a failure or timeout is
`503`. A malformed id (not 22 base62 characters) is `404` before any lookup, so junk cannot cost a
read. The alternative, not declaring the marker, would cost a goroutine and two scheduler hand-offs
on every call to protect against a miss that is already bounded.

## Consequences

Easier: the hot path has no goroutine hop. Harder: the "never blocks" contract is relaxed to "blocks
at most 250 ms once per workspace per cache lifetime"; a stalled database makes first calls 503
rather than hang. The tradeoff favours the client stream (non-negotiable 1) over strictness of the
marker. Revisit if tollgate adds a per-call marker or a warm-cache API. Enforced by
`TestLookupFailureAndTimeoutAre503` and the `InlineSafe` interface assertion in `internal/wsauth`.
