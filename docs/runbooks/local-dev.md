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
export METERWISE_DATABASE_URL="$TEST_DATABASE_URL"
go run ./cmd/meterwised config check -json     # lists every configuration problem at once
go run ./cmd/meterwised serve                  # /healthz and /api/v1/version in the current build
curl -s 127.0.0.1:8080/healthz
```

Environment: `METERWISE_LISTEN` (default `:8080`), `METERWISE_DATABASE_URL` (required), `METERWISE_LOG_LEVEL`
(`debug|info|warn|error`), `METERWISE_ENV` (`dev|test|prod`), `METERWISE_MAX_WORKSPACES` (default 20).

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
| Regenerate | `go run ./cmd/gen` (and `go tool -modfile=tools/go.mod sqlc generate` once sqlc is configured) |
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
