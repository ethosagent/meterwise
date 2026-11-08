# 0005. Postgres only, with pgx, sqlc and goose

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 00 D9

## Context

Minimal infrastructure for a single-tenant start. Options: Postgres plus Redis plus a vector store, Postgres only (chosen).

## Decision

Postgres 16 is the only store: `pgx` for access, `sqlc` for queries, `goose` for migrations. No row-level security in this plan set; no Redis, no vector store.

## Consequences

Fewer moving parts; pgvector can join later. RLS returns with multi-org. Enforced by: H6 allowlist (pgx, goose only in `store`), the `sqlc`, `migrate` and `generated` gates, tools pinned in `tools/go.mod`.
