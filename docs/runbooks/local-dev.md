# Local development

## Prerequisites

Go (see `go.mod`), Node 22+ with npm, Docker (for Postgres 16), `psql`, `jq`. Dev tools are pinned: Go tools in
`tools/go.mod` (run through `scripts/lib.sh` `gotool`), web tools in `web/package.json`. Nothing is assumed installed.

## First run

```sh
scripts/install-hooks.sh                # git hooks for THIS clone or worktree only; never global config
make db-up                              # Postgres 16 on 127.0.0.1:54329 (docker compose, project "meterwise")
export TEST_DATABASE_URL='postgres://meterwise:meterwise@127.0.0.1:54329/meterwise?sslmode=disable'
scripts/run-checks.sh                   # the full gate; CI runs exactly this
```

`make db-down` stops Postgres. Set `METERWISE_PG_PORT` to move the host port. The `migrate` gate and database
tests create and drop their own throwaway databases; they never touch the `meterwise` database's data.

## Run the binary

```sh
export DATABASE_URL="$TEST_DATABASE_URL"
go run ./cmd/meterwised config check -json     # lists every configuration problem at once
go run ./cmd/meterwised migrate                # goose baseline (0001_store) against DATABASE_URL
go run ./cmd/meterwised serve                  # proxy :8080, API + web :8081, admin 127.0.0.1:9090
curl -s 127.0.0.1:8081/healthz                 # {"status":"ok"} (pings Postgres)
curl -s 127.0.0.1:8081/api/config              # {"appName":...,"proxyPublicHost":...}
curl -s 127.0.0.1:8080/readyz                  # tollgate's own probe on the proxy listener
curl -s 127.0.0.1:9090/statusz                 # tollgate admin; keep this listener private
```

Proxy dev mode (`tollgate.NoAuth`, one tenant): point an agent at `http://127.0.0.1:8080/openai/v1` (or `/anthropic`)
with its own provider key. The per-workspace `/w/{id}/...` URL arrives in Plan 05. The control plane resolves every
request to a stub admin until Plan 02.

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | required | `postgres://` URL |
| `LISTEN_PROXY` / `LISTEN_API` / `LISTEN_ADMIN` | `:8080` / `:8081` / `127.0.0.1:9090` | must be distinct ports |
| `PROXY_PUBLIC_HOST` / `APP_NAME` | `localhost:8080` / `Meterwise` | returned by `GET /api/config` |
| `PRIVACY_MODE` | `true` | tollgate privacy mode (decision D12) |
| `MAX_WORKSPACES` / `MAX_CONCURRENT_PER_WORKSPACE` | `20` / `8` | operational limits (D13) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | base URL of an OTLP/HTTP collector; unset turns tracing off |
| `CLERK_ISSUER` / `CLERK_JWKS_URL` / `CLERK_PUBLISHABLE_KEY` | unset | consumed by Plan 02 |
| `WEB_DIR` | `web/dist` | built web app served on `LISTEN_API` |
| `LOG_LEVEL` / `APP_ENV` | `info` / `dev` | `debug|info|warn|error` / `dev|test|prod` |

## Web

```sh
cd web && npm ci && npm run test && npm run build
```

## Everyday commands

| Task | Command |
|---|---|
| Fast loop | `scripts/run-checks.sh --fast` |
| One gate | `scripts/run-checks.sh arch` (logs in `.logs/arch.log`) |
| Quiet test run | `scripts/quiet.sh go test -race ./internal/config` |
| Regenerate | `go run ./cmd/gen` (and `make sqlc` after changing `internal/store/queries` or a migration) |
| Risk tier of a change | `scripts/risk-tier.sh $(git diff --name-only HEAD)` |

## Tests and the network

Tests are offline: `-tags=offline`, `GOFLAGS=-mod=readonly`, and each package's `TestMain` installs `internal/nonet`,
which refuses non-loopback dials and DNS. Postgres on loopback is allowed. Clerk and provider upstreams exist only as
mock twins. Without `TEST_DATABASE_URL` the database tests print `! SKIP`; under `CI=true` they fail instead.

## Agent setup (outside the repo, not committed)

Put this in `~/.claude/settings.json` so every session starts lean:

```json
{
  "env": {
    "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "400000",
    "BASH_MAX_OUTPUT_LENGTH": "15000",
    "MAX_MCP_OUTPUT_TOKENS": "15000",
    "CLAUDE_CODE_FILE_READ_MAX_OUTPUT_TOKENS": "15000"
  }
}
```

The repo's `.claude/settings.json` carries the same block. Keep the global `CLAUDE.md` at 40 lines or fewer. For this repo
**disable** the connectors you do not need for coding (Gmail, Calendar, Drive, Slack, Notion, Shopify, finance tools):
each connected MCP server adds tool definitions to every session's context. Keep GitHub and the one tracker you use.
Measure with `/context` ([`agent-hygiene.md`](agent-hygiene.md)).
