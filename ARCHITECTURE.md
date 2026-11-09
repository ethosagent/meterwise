# Meterwise architecture (the constitution)

> Protected path (`.agents/risk.yaml`, tier gates). Changing it needs a maintainer. Keep under 300 lines.
> Rules here are enforced by a gate, a hook or a test named next to each; prose alone is not enforcement.

## 1. What this is

Meterwise is an LLM-proxy SaaS that saves agent tokens and shows exactly where. This repo is the **host**:
it embeds the `github.com/ethosagent/tollgate` library (the proxy), and adds auth, tenancy, monitors, a
ledger and a dashboard. One Go binary (`cmd/meterwised`) serves the proxy and the control-plane API; a React
app in `web/` is the UI; Postgres is the only store. Single tenant for now, multi-tenant-ready (decision D4).

## 2. Vocabulary (use these words, grep for them)

| Term | Meaning |
|---|---|
| Org | The one seeded organization. Every table keeps `org_id` so multi-org is additive. |
| Workspace | The unit of proxying: own URL, sessions, savings, monitor config. tollgate `Tenant.ID` = workspace id. |
| Public id | The unguessable 22-char base62 id in `/w/{id}/{provider}/...`. It is the only credential (D7). |
| Principal | An authenticated human: user id, org id, role `admin | member`. |
| Call | One proxied provider request/response. The ledger row is keyed by `call_id`. |
| Session | A run of related calls in one workspace (tollgate session strategy). |
| Ring | The bounded per-session history of recent calls that monitors read. |
| Monitor | A hook check that finds wasted calls; mode `observe | note | block` per workspace. |
| Saved | Dollars from **blocked** calls: certain, auditable to one call. |
| Likely saved | Dollars from shorter sessions: estimated. **Never summed with Saved** (D11). |
| Mock twin | An in-repo fake of an adapter with committed fixtures (`clerkmock`, `pgtest`, tollgate `testkit`). |

## 3. Layers and the dependency law

Enforced by `internal/archtest` (gate `arch`), each rule with a seeded violation that must go red.

```
cmd/*                         rank 5   binary and tools; wires everything
internal/api                  rank 4   control-plane HTTP
internal/{dataplane,history,monitors,ledger}   rank 3   call path
internal/{identity,tenancy,access,wsauth,audit,savings}   rank 2   domain
internal/{config,store}       rank 1   base
internal/{contract,nonet}     rank 0   kernel and test guard
internal/archtest             standalone: standard library only, imported by nobody
```

| Rule | Statement |
|---|---|
| D1 | Every package belongs to a layer (a row in `law_test.go` and the table above). |
| P1 | Every package has a doc comment (`doc.go`); that is its README. |
| H0 | `internal/contract` imports the standard library and the tollgate root only. |
| H1 | tollgate's public surface only: its root, `ext/**`, and `testkit` from tests. Never `internal/`, `cmd/` or anything else. |
| H2 | Lower never imports higher (ranks above). Nothing imports a `cmd/*` or `archtest`. |
| H3 | `history`, `monitors`, `savings` are pure: stdlib (no `net`, `database/sql`, `os/exec`), tollgate root, `contract`. |
| H4 | Only `ledger` and `audit` write `calls`, `sessions`, `monitor_events`, `audit_log` (INSERT, UPDATE, DELETE, MERGE, TRUNCATE, COPY over `.go`/`.sql`, comments ignored). |
| H5 | `web/` imports `web/src/**` and the npm allowlist only (any JS/TS extension, template-literal imports included; `node:` only in tests); `package.json` declares nothing else. |
| H6 | A third-party Go import needs an allowlist entry per layer in `law_test.go` and an ADR (0007). |
| H7 | No `//go:build`, `// +build` or GOOS/GOARCH file suffix: a constraint can hide code from every gate. Allowlist per file with an ADR. |
| H8 | Every package with tests has a `TestMain` that really calls `nonet.Run` (checked on the AST, a comment does not count): no test reaches the external network by name. |
| H9 | One module plus `tools/`: a nested `go.mod` or any `go.work` hides packages from every gate. |

The arch gate runs `go list` with `-mod=readonly`: it never downloads a module or rewrites `go.mod`.

Adding a dependency, a layer or a rule edits `internal/archtest` and `ARCHITECTURE.md` in one change, with an ADR.

## 4. Module map

| Path | Role | Rank |
|---|---|---|
| `cmd/meterwised` | Binary: `serve`, `config check`, `bootstrap-admin`, `migrate`, `version`; every subcommand takes `-json` | 5 |
| `cmd/gen` | Generator and lint: contract TS twin, openapi.json, docs/schema.md, migration lint (`-check` for the gate) | 5 |
| `internal/api` | Handlers, gateway middleware; routes come from the contract table | 4 |
| `internal/dataplane` | tollgate wiring: authenticator, resolver, admission, options, limits | 3 |
| `internal/history` `monitors` | Per-session ring; six checks (pure, hot path) | 3 |
| `internal/ledger` | `Hook.After` to `calls`, `sessions`, `monitor_events` (the writer, D8) | 3 |
| `internal/identity` | `IdentityProvider` port, Clerk adapter, `clerkmock` twin | 2 |
| `internal/tenancy` `access` `audit` | Org and workspaces; RBAC kernel; append-only audit | 2 |
| `internal/wsauth` | `/w/{id}` to tenant; reset and grace handling | 2 |
| `internal/savings` | Pricing snapshot, estimator (pure) | 2 |
| `internal/config` | Environment to validated `Config`; reports every problem | 1 |
| `internal/store` | sqlc output, pgx pool, goose migrations, `pgtest` twin | 1 |
| `internal/contract` | Errors registry, ports, route table, DTOs, `MicroUSD` | 0 |
| `internal/nonet` | Test guard: external network blocked, loopback open | 0 |
| `web/` | React app; `web/src/features/<name>/` vertical slices; generated contract twin | n/a |

## 5. Non-negotiables

1. **Client-first streaming.** tollgate writes bytes to the client before any hook sees them. No host hook may stall, reorder or break a stream (`STR-*`).
2. **No I/O in `Before`.** Slow work goes to a bounded queue; a broken hook degrades to pass-through, never to an outage.
3. **Never hold provider keys.** `Authorization` and `x-api-key` pass through verbatim; never stored, never logged (`PRIV-2`).
4. **Privacy mode on by default** (D12). Metadata only unless a workspace opts in to capture.
5. **One writer per ledger table** (D8, H4), idempotent on `call_id` (`DATA-1`).
6. **Two savings numbers, never summed** (D11, `DATA-3`).
7. **Money is int64 micro-USD** in Go, `numeric(14,6)` `*_usd` in Postgres, a decimal string in JSON (R12).
8. **Every query is scoped** by `workspace_id` or `org_id`; every route has a permission row (`TNT-*`).
9. **No globals, no `init()` magic.** Wiring is code in `cmd/meterwised`; `os.Getenv` only in `cmd/`.
10. **Applied migrations are immutable.** New file only; one feature's tables per file.
11. **Generated stays generated.** Hand edits fail the `generated` gate.
12. **Tests are offline.** Loopback only (`nonet`, required by H8), mock twins for Clerk and providers, `pgtest` for Postgres.

## 6. Seams (ports in `internal/contract`)

| Port | Implemented by | Mock twin |
|---|---|---|
| `IdentityProvider` | Clerk adapter (JWKS) in `identity` | `identity/clerkmock` (committed token fixtures) |
| `PrincipalResolver`, `OrgDirectory`, `WorkspaceDirectory` | `tenancy`, `access` over `store` | in-memory fakes in the plan that adds them |
| `AuditSink` | `audit` | in-memory sink |
| `Pricer` | `savings` snapshot (pure); loader in `ledger` (R11) | table fixture |
| tollgate `Authenticator`, `ProviderResolver`, `Admission`, `Hook` | `wsauth`, `dataplane`, `monitors`, `ledger` | `testkit.FakeUpstream` |
| Postgres | `store` (pgx) | `store/pgtest` over a loopback Postgres (`TEST_DATABASE_URL`) |

A gate whose inputs do not exist yet prints `! SKIP <gate>: <reason>` and fails closed once they do (R5).

## 7. Generated artifacts (gate `generated`)

| File | Source | Command |
|---|---|---|
| `web/src/contract/generated.ts` | `internal/contract` | `go run ./cmd/gen` |
| `internal/api/openapi.json` | route table and DTOs | `go run ./cmd/gen` |
| `docs/schema.md` | `internal/store/migrations` | `go run ./cmd/gen` |
| `internal/store/sqlc/**` | `internal/store/queries`, migrations | `sqlc generate` (pinned in `tools/`) |
| RBAC taxonomy twin | `internal/contract/taxonomy.go` (from the access plan) | `go run ./cmd/gen` |

They are denied to whole-file reads in `.claude/settings.json` and `read-guard.sh`; read the source instead.

## 8. Gates

`fmt vet mod arch lint test vuln docs size sqlc migrate web generated`, run by `scripts/run-checks.sh`
(local, pre-push and CI are the same script). `--fast` runs `fmt vet arch docs size sqlc`. Coverage floors:
Go 75 %, web 70 %; raise, never lower. Dev tools (golangci-lint, govulncheck, sqlc, goose) are pinned in
`tools/go.mod`; web tools in `web/package.json` with exact versions.

## 9. Amendment procedure

Change this file, `internal/archtest` and the matching ADR together. A maintainer approves (protected paths:
see `CLAUDE.md`). Never edit an accepted ADR's decision; supersede it. Never make a gate pass by loosening it.
