# Memory (the trace library)

Memory distils finished sessions into traces, recalls a similar one at the start of a later session and appends one
hint line to that request. Design: [ADR 0031](../decisions/0031-pgvector-for-trace-recall-text-literals-a-domain-and-a-cast-index.md),
[0032](../decisions/0032-memory-recalls-off-the-request-path-and-credits-only-a-likely-number.md),
[0033](../decisions/0033-memory-has-its-own-model-key-client-and-budget.md).

## Turn it on

1. Postgres has pgvector: [database-roles.md](database-roles.md#pgvector-once-per-database-before-the-first-migrate)
   (`pgvector/pgvector:pg16` in compose and CI).
2. Set the key and model in the server's environment (never in a workspace setting):

   | Variable | Default | Meaning |
   |---|---|---|
   | `MEMORY_API_KEY` | unset | Meterwise's own key for an OpenAI-compatible API. Unset = memory off, `disabledReason=no_key` |
   | `MEMORY_BASE_URL` | `https://api.openai.com/v1` | API root, no query, no credentials |
   | `MEMORY_MODEL` / `MEMORY_EMBED_MODEL` | `gpt-4o-mini` / `text-embedding-3-small` | distil model; embedding model (1 536 dimensions) |
   | `MEMORY_SELF_BUDGET_USD` | `1` | per workspace per UTC day; at the limit memory pauses (`disabledReason=budget`) |
   | `MEMORY_TRACE_RETENTION_DAYS` | `90` | traces unused this long are deleted hourly |

   `meterwised config check -json` shows the key as `set` or `unset`, never its value.
3. Per workspace: turn capture on (memory follows the ledger's text rule; with privacy mode on it says `privacy`),
   then Settings > Monitors > Remembered solutions: `observe` records what it would hint, `hint` injects it.

## Reading it

- `GET /api/workspaces/{id}/memory/stats?range=` hits, observed hits, feedback, likely money, memory's own spend.
- A session detail lists `memoryHits`; the hint is re-rendered from the trace.
- `/statusz` (admin listener) `memory`: queue depth, jobs done/failed/dropped by kind, hints injected, skips by reason.
- A hint first lands on call 2 of a session (recall runs after call 1, off the request path).

## Forget and retention

`DELETE /api/workspaces/{id}/traces` removes every trace of the workspace (audited as `memory.forget`) and drops hints
still waiting in sessions. Retention is the hourly prune above.

## When it does not work

| Symptom | Check |
|---|---|
| Settings say `no_key` | `MEMORY_API_KEY` is set in the server's environment and the server restarted |
| `privacy` | workspace capture is off, or workspace/global privacy mode is on |
| `budget` | `memory_self_usage` for today; raise `MEMORY_SELF_BUDGET_USD` or wait for 00:00 UTC |
| `migrate` fails at 0024 | the superuser step in database-roles.md was skipped |
| No hints, no errors | `/statusz` `memory.jobs.recall` and `skipped_*`; a session needs 4+ calls and a write tool to become a trace |
