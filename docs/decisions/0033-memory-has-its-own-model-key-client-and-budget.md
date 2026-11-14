# 0033. Memory has its own model key, its own client and a daily budget

- **Status:** Accepted
- **Date:** 2026-11-12
- **Source:** Plan 09, rulings R2 and R15; non-negotiable 2 (never hold provider keys)

## Context

Meterwise forwards the customer's own `Authorization` / `x-api-key` and never stores it. Memory is the one feature
that must call a model for itself (distil a session, embed a task). That needs a key that belongs to Meterwise, and it
must not be possible to mix it up with the customer's.

## Decision

1. **The key comes from the environment only.** `MEMORY_API_KEY`, `MEMORY_BASE_URL` (default
   `https://api.openai.com/v1`), `MEMORY_MODEL` (default `gpt-4o-mini`), `MEMORY_EMBED_MODEL` (default
   `text-embedding-3-small`, 1 536 dimensions). It is never logged, never in a DTO, never in an error text, and
   `config check -json` shows `set` or `unset`. A workspace may name a model, never a key or an endpoint.
2. **No key, no memory.** The hook is not built, nothing is captured or recalled, and the settings answer
   `disabledReason = no_key`. The read API still lists and forgets traces.
3. **One client, one rule (H12).** Only `internal/memory`, `internal/identity` (the Clerk adapter) and `cmd/**` may
   name `http.Client`, `http.DefaultClient` or the `http.Get` family. The archtest rule scans non-test Go for it and a
   seeded violation keeps it honest. Memory's client has a 10 s timeout, an explicit Transport, a 1 MB response cap,
   and sets the key per request.
4. **Budget.** `MEMORY_SELF_BUDGET_USD` (default 1) is per workspace per UTC day, read from `memory_self_usage`. At the
   limit, jobs stop calling the model, count `skipped_budget`, and the settings say `budget`. Our own calls are priced
   with the host's price table for the model; an unpriced model counts a flat 1 micro-USD per token (an overestimate).
5. **Redaction before any model and before any store.** Strings shown to the model, and every string stored in a trace,
   pass `memory.Redact`: `sk-ant-`, `sk-`, `Bearer` tokens, AWS, GitHub and Slack tokens, and `key=value` pairs whose key
   names a secret. The distiller's JSON is parsed strictly (unknown field, missing field or trailing text is an error),
   clamped, redacted, and redacted again by the engine.
6. **Retention.** `MEMORY_TRACE_RETENTION_DAYS` (default 90): an hourly job deletes traces not used since the cutoff
   and old failure records. `DELETE /api/workspaces/{id}/traces` forgets a workspace's traces at once and drops waiting
   hints.

## Consequences

- A second key to manage and rotate; memory costs Meterwise money, bounded by the budget.
- Redaction is pattern-based and can miss a novel secret shape. The summary is small, which limits what could leak.
- Tests and the demo use loopback twins only; no real model key exists in the repo or CI.
- Enforced by: archtest H12 (`TestSeededHTTPClientsGoRed`), `config` tests, `internal/memory` tests
  (`TestModelErrorsNeverCarryTheKeyOrTheBody`, `TestRedact`, `TestParseTraceIsStrict`).
