# Conventions

## Go

- Standard library first; a third-party import needs an archtest allowlist entry and an ADR (H6).
- Functions at most 50 lines; wrap errors with `%w`; compare with `errors.Is/As`.
- No globals, no `init()`. Configuration arrives through `internal/config` (an injected lookup); `os.Getenv` only in `cmd/`.
- Logging: the injected `*slog.Logger`, lines carry ids: `workspace session call provider model`. Never bodies or credentials.
- HTTP clients and servers set timeouts; `http.DefaultClient`, `http.DefaultTransport`, `http.Get` are banned by lint.
- Errors that reach a client are `*contract.Error` from the registry; use `contract.AsError` at the edge.
- Every package has `doc.go` saying what it does and what it may import (P1).
- Tests: table-driven, `TestMain` with `nonet.Run`, mock twins, no `time.Sleep` for ordering, injected clocks.

## SQL and sqlc

- One goose migration per feature (`NNNN_<feature>.sql`, `-- feature:` header, real Down); one queries file per feature.
- Every table has `org_id`; workspace-owned tables also `workspace_id`; timestamps are `timestamptz` in UTC.
- Every query filters by `workspace_id` or `org_id`. Only `queries/ledger.sql` and `queries/audit.sql` write the ledger tables.
- sqlc output (`internal/store/sqlc`) is generated, never edited; `sqlc diff` is part of gates `sqlc` and `generated`.

## Money

- Go: `contract.MicroUSD` (int64 millionths of a dollar). Never `float32`/`float64` for money.
- Postgres: `numeric(14,6)`, column names end `_usd`. JSON: an integer count of micro-USD such as `1500`.
- Saved (blocked calls, certain) and likely saved (estimated) are separate fields everywhere and never added together.

## TypeScript

- Strict `tsc`, biome format and lint, `noExplicitAny`, `noBarrelFile`; no `index.ts` re-export chains.
- One folder per feature under `web/src/features/<name>/`; server state in TanStack Query, UI state in Zustand.
- Types for API data come from `web/src/contract/generated.ts`. Design tokens only.
- npm packages must be on the allowlist in `internal/archtest` (H5) with an ADR. Versions in `package.json` are exact.

## Git

- Branch `wave/<plan>-<name>` or `feature/...`; one logical change per commit; never push from an agent session.
- Commit messages: imperative subject, why in the body, attribution trailers at the end.
- Hooks: `scripts/install-hooks.sh` sets `core.hooksPath` for the current clone or worktree only.
