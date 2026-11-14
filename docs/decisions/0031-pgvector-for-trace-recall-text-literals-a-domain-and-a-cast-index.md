# 0031. pgvector for trace recall: text literals, a domain, and an index on a cast

- **Status:** Accepted
- **Date:** 2026-11-12
- **Source:** Plan 09 (trace library), ruling R13; migration `0024_memory.sql`

## Context

Recall needs nearest-neighbour search over embeddings of past sessions, scoped to one workspace, in a few
milliseconds at 100 000 traces. Postgres is the only store (ARCHITECTURE.md), so the choice is the `pgvector`
extension. Three things follow from that choice.

- `sqlc` maps a `vector` column to `pgvector.Vector`, which lives in a module this repo does not import (H6: a
  third-party import needs an allowlist entry and an ADR).
- pgvector is **not** a trusted extension (`pg_available_extension_versions.trusted` is false for 0.8.7), so a
  non-superuser migrate role cannot run `CREATE EXTENSION vector`.
- An HNSW index needs the column's dimension. A column typed by a domain hides it ("column does not have dimensions").

## Decision

1. **No Go vector dependency.** Vectors cross the driver as text literals (`[0.1,0.2,...]`) and are cast in SQL
   (`$1::text::vector(1536)`). `store.vectorLiteral` refuses a wrong dimension and a NaN or infinite value.
2. **A domain for sqlc.** `memory_traces.embedding` is `memory_embedding`, `CREATE DOMAIN ... AS vector(1536)`. sqlc
   sees an opaque type and generates plain Go; the database enforces the dimension.
3. **The index is on a halfvec cast.** `USING hnsw ((embedding::halfvec(1536)) halfvec_cosine_ops)`, and the recall
   query orders by the same expression so the planner uses it. The column and the reported similarity
   (`1 - distance`, computed for the returned rows) stay full precision; only the graph is 16-bit. A `vector(1536)`
   graph for 100 000 traces is about 780 MB and did not fit the 128 MB test buffer cache (database p99 6.0 ms); the
   halfvec graph is half that (database p99 4.1 ms). Needs pgvector 0.7 or newer (0.8.7 here).
4. **The extension is the operator's step.** The migration says `CREATE EXTENSION IF NOT EXISTS vector`, which is a
   no-op once a superuser has created it. `docs/runbooks/database-roles.md` gains the step: a superuser runs it once
   per database before the first `migrate`; `meterwise_owner` and `meterwise_app` stay non-superuser and the serve
   guard is unchanged. The pgtest `TestMigrateAsNonSuperuserOwnerWithPreCreatedVector` proves it.
5. **Down drops the extension.** The migrate gate compares `pg_dump` before Up and after Down, so Down must restore
   the schema exactly, extension included. Dropping needs a superuser; a non-superuser who rolls back asks a DBA.
6. **Recall is one workspace.** The query filters `workspace_id` and `confidence >= 0.6`. With the filter applied after
   the index scan, `hnsw.iterative_scan = relaxed_order` keeps a small workspace from starving, and
   `hnsw.ef_search = 20` (the pgvector default is 40; 40 gave a database p99 of 4.1 to 5.8 ms on a busy laptop, 100 took 450 ms). The
   settings and the query travel in one batch (one round trip) and last for its implicit transaction;
   `plan_cache_mode = force_custom_plan` keeps a prepared statement from a generic plan, which cannot see the constant
   vector and scans sequentially (10 s). Older servers without these settings run the query untuned.
7. **Local and CI Postgres** use `pgvector/pgvector:pg16` (same major version, volume compatible).

## Consequences

- No new module; H6 is untouched. The cost is a text round trip of 1 536 floats per query.
- Measured on a laptop (Docker Postgres, 128 MB shared buffers, 100 000 traces of random 1 536-d vectors, the worst
  case for HNSW): the query's own execution time is p50 1.2 ms, p99 3.4 ms (best of 5 trials), and the bench asserts that p99 under 5 ms.
  End to end through the Go driver and the Docker port forward it is p50 2.9 ms, p99 6 to 11 ms depending on machine load, reported and not
  asserted: the budget belongs to the database, the network hop is the deployment's. Recall runs off the request
  path (R1), so neither figure is on a request's latency.
- HNSW is approximate. A trace may miss its own vector (5 to 14 % of the bench's random-vector queries at ef_search 20); real task
  embeddings cluster and miss far less, and a miss is a missed hint, never a wrong one.
- The query text lives in `store/memory_recall.go`, not `queries/`, so it can share a batch with the settings; the
  bench holds a copy of it under EXPLAIN ANALYZE.
- A fresh database without the extension fails `migrate` with a plain error until the runbook step is done.
- Enforced by: `store` tests `TestTracesAreStoredOncePerSessionAndRecalledPerWorkspace`,
  `TestRecallP99UnderFiveMillisecondsAt100kTraces`, the `migrate` gate (up/down/up), and the pgtest role test above.
