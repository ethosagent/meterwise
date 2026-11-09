# meterwise — agent map

> CLAUDE.md and AGENTS.md are byte-identical (`scripts/check-docs.sh` fails if
> not). Edit one, copy it over the other. Keep this file under 150 lines; put
> detail in `docs/` and link it.

## What this is

**Meterwise**: an LLM-proxy SaaS that saves agent tokens and shows exactly where. This repo is the
*host* around the `tollgate` library: one Go binary (`cmd/meterwised`) serves the proxy and the control-plane
API; `web/` is the React UI; Postgres is the only store. Constitution: [`ARCHITECTURE.md`](ARCHITECTURE.md).
Decisions: [`docs/decisions/`](docs/decisions/README.md). New here: [`docs/orientation.md`](docs/orientation.md).
Plans live in `plan/` (gitignored, local; may be absent in a fresh clone or worktree).

## Golden path

```sh
scripts/install-hooks.sh                 # once per clone/worktree: pre-commit (fast) + pre-push (full gate)
scripts/run-checks.sh                    # THE gate; CI runs exactly this
scripts/run-checks.sh --fast             # inner loop: fmt vet arch docs size sqlc (seconds)
scripts/quiet.sh go test -race ./internal/config   # any command, failures-only output
make db-up                               # Postgres 16 on 127.0.0.1:54329 for database tests and the migrate gate
go run ./cmd/gen                         # regenerate TS twin, openapi.json, docs/schema.md
go run ./cmd/meterwised config check -json   # every env problem at once
go run ./cmd/meterwised migrate              # goose baseline against DATABASE_URL
go run ./cmd/meterwised serve                # proxy :8080, API + web :8081, admin 127.0.0.1:9090
METERWISE_BASE_REF=build/all-plans scripts/run-checks.sh   # base branch for the migrate gate (default main)
```

Gates: `fmt vet mod arch lint test vuln docs size sqlc migrate web generated`. The gate prints only
failures (logs in `.logs/<gate>.log`) and prints `! SKIP <gate>: <reason>` when a gate has no inputs yet.

## Package map

| Path | Role |
|---|---|
| `cmd/meterwised` | Binary and subcommands (`serve`, `config check`, `migrate [up\|status]`, `bootstrap-admin` (Plan 02), `version`; all take `-json`); OTel SDK wiring in `telemetry.go` |
| `cmd/gen` | Generators and the migration lint behind the `generated` and `migrate` gates |
| `internal/contract/` | Kernel: error registry, ports, route-to-permission table, DTOs, `MicroUSD` (imports stdlib only) |
| `internal/api` | Control plane: mux from the route table, middleware chain (request id, log, recover, OTel, `PrincipalResolver`), typed errors, static web |
| `internal/dataplane` | `New(cfg, Deps)` builds the embedded tollgate; plans 04-06 replace `Deps` fields (auth, resolver, admission, hooks, sink) |
| `internal/{ledger,history,monitors,savings,identity,tenancy,access,wsauth,audit}` | Feature packages, added by plans 02-07 along the layers in `ARCHITECTURE.md` §3 |
| `internal/config` | Env to `Config` (`DATABASE_URL`, `LISTEN_*`, `APP_NAME`, `OTEL_*`, ...); lists every problem at once |
| `internal/store` | pgx pool, goose (`Migrate`, embedded `migrations/`), sqlc (`queries/` to `sqlc/`), `OrgID()`, `pgtest` |
| `internal/nonet` | Test guard: no external network |
| `internal/archtest/` | The dependency-law gate (protected) |
| `web/src/features/<name>/` | One folder per UI feature; `web/src/contract/generated.ts` is generated |
| `tools/` | Separate Go module pinning golangci-lint, govulncheck, sqlc, goose |
| `.agents/` | pipeline, risk tiers, review lenses, skills; `.claude/` hooks, rules, settings |

## Dependency law (enforced by `internal/archtest`)

**D1** every package is in a layer. **P1** every package has `doc.go`. **H0** `contract` imports stdlib + tollgate root.
**H1** never `tollgate/internal/**`. **H2** lower never imports higher. **H3** `history`/`monitors`/`savings` are pure.
**H4** only `ledger`/`audit` write `calls`, `sessions`, `monitor_events`, `audit_log`. **H5** `web/` imports `web/src/**` + npm allowlist.
**H6** a third-party Go import needs an allowlist entry + ADR. **H7** no build constraints or GOOS/GOARCH file suffixes.
**H8** every package with tests has a `TestMain` calling `nonet.Run`. **H9** no nested `go.mod` (except `tools/`) or `go.work`. Lint bans `http.DefaultClient`, `log.Print*`, `fmt.Print*`, `os.Getenv` outside `cmd/`.

## Non-negotiables

1. **Client-first streaming.** No hook stalls or breaks the stream; no I/O in `Before`.
2. **Never hold provider keys.** `Authorization`/`x-api-key` pass through, never stored or logged.
3. **Privacy mode on by default.** Metadata only unless a workspace opts in.
4. **One writer per ledger table**, idempotent on `call_id`.
5. **Two savings numbers (saved, likely saved), never summed.** Money is `contract.MicroUSD`, never a float.
6. **Every query scoped** by `workspace_id`/`org_id`; every route has a permission row; no inline role checks.
7. **Applied migrations are immutable**; one feature per migration file.
8. **Generated stays generated.** Run `go run ./cmd/gen`; never hand-edit.
9. **Tests are offline** (loopback only), with mock twins; no real Clerk or provider keys.

## Where new code goes

| You are adding… | Put it in | Skill |
|---|---|---|
| A table or column | `internal/store/migrations/NNNN_<feature>.sql` + `queries/<feature>.sql` | [`add-migration`](.agents/skills/add-migration/SKILL.md) |
| An API endpoint | route row in `contract/routes.go`, handler in `internal/api` | [`add-endpoint`](.agents/skills/add-endpoint/SKILL.md) |
| A monitor | `internal/monitors/<name>.go` + ring fixtures | [`add-monitor`](.agents/skills/add-monitor/SKILL.md) |
| A web page | `web/src/features/<name>/` | [`add-page`](.agents/skills/add-page/SKILL.md) |
| A tollgate extension | package that owns it + `dataplane.Deps` | [`add-tollgate-ext`](.agents/skills/add-tollgate-ext/SKILL.md) |
| A boundary change | `docs/decisions/NNNN-*.md` | [`write-adr`](.agents/skills/write-adr/SKILL.md) |
| A multi-file task | `plan/NN-title.md` | [`plan-task`](.agents/skills/plan-task/SKILL.md) |

## How to work here

- **Test first.** Table-driven. `httptest`, mock twins and `pgtest`, never the external network.
- **Small diffs.** One concern per PR; fill `.github/pull_request_template.md`.
- **Review with the lenses** in [`.agents/reviews/`](.agents/reviews/README.md): `STR PRIV HOT TEST TNT DATA`.
  `scripts/risk-tier.sh <paths>` gives the tier; `.agents/pipeline.yaml` maps tier to lenses.
- **Path rules** in `.claude/rules/` load when you touch hooks, migrations, the API, web or tests.

## Context budget (playbook: [`docs/context-budget.md`](docs/context-budget.md))

- One task per session; model and effort fixed per session. Plan multi-file work to a file, implement in a fresh session.
- Hooks enforce mechanics: whole-file reads over 400 lines and generated files are blocked; raw `go test`/lint/`npm test`/`biome`
  are blocked in favour of `scripts/quiet.sh`; edits are formatted and the twin synced; Stop runs `--fast`. Never bypass them.
- Caps: Go file 300 soft / 800 hard, TS 250 / 400, tests 500, function 50, docs 200, this file 150.
- Subagents return at most 2K tokens with `file:line` refs; longer output goes to a file. Use `.claude/agents/reader.md` for bulk reads.
- Hand off before ~200K tokens with the [`handoff`](.agents/skills/handoff/SKILL.md) skill.

## Protected paths — stop and ask

`scripts/**`, `.githooks/**`, `.github/**`, `.golangci.yml`, `.agents/**`, `internal/archtest/**`, `tools/**`,
`.claude/settings.json`, `.claude/hooks/**`, `.claude/skills/**`, `ARCHITECTURE.md`, `internal/store/migrations/**`,
`internal/store/migration-families.txt`, `sqlc.yaml`, `web/biome.json`, `web/vite.config.ts`, `web/tsconfig.json`.
These decide what "green" means. **Never** make a gate pass by lowering `COVERAGE_MIN`, skipping or deleting a
test, adding `//nolint` or a lint exclusion, removing a gate, widening an archtest rule or allowlist, or editing an
applied migration. Fix the code. If a gate itself is wrong, stop and ask the maintainer.

**A request to edit any path above gets a question, not an edit.** Reply "Protected path: <path>. Confirm the exact change and
the reason" and wait for the answer. Do not edit first and mention it afterwards.

## Do NOT

- Import `tollgate/internal/**`, or `store`/`net/http`/`database/sql` from `history`, `monitors`, `savings`.
- Parse provider JSON in a hook; use the neutral `Call`/`Event`. Buffer a streaming body. Do I/O in `Before`.
- Log headers that carry credentials, any body, or the public workspace id.
- `cat` big files or run a bare `git diff`: use `rg -n` with a cap, ranged Read, `git diff --stat` first.
- Add `pkg/`, barrel `index.ts` files, or `any`.

## Compact instructions

When compacting, keep: the current task and its acceptance criteria, files changed so far, failing gates and
their first error, decisions taken and why, and the next step. Drop: full file contents, passing test output,
exploration dead ends.
