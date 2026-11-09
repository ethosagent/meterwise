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
export APP_ENV=dev
go run ./cmd/meterwised serve                  # proxy :8080, API + web :8081, admin 127.0.0.1:9090
curl -s 127.0.0.1:8081/healthz                 # {"status":"ok"} (pings Postgres)
curl -s 127.0.0.1:8081/api/config              # {"appName":...,"proxyPublicHost":...}
curl -s 127.0.0.1:8080/readyz                  # tollgate's own probe on the proxy listener
curl -s 127.0.0.1:9090/statusz                 # tollgate admin; keep this listener private
```

Proxy dev mode (`tollgate.NoAuth`, one tenant): point an agent at `http://127.0.0.1:8080/openai/v1` (or `/anthropic`)
with its own provider key. The per-workspace `/w/{id}/...` URL arrives in Plan 05.

Identity in dev: with no `CLERK_*` variables and `APP_ENV=dev`, the control plane signs every request in as a seeded
dev admin (`dev-admin@meterwise.local`, a real user and membership row) so the web app runs without a Clerk account.
Set `CLERK_ISSUER` and `CLERK_JWKS_URL` and the real adapter takes over. `serve` creates the default workspace at boot
and needs the schema, so run `meterwised migrate` first. See [`clerk.md`](clerk.md) for the Clerk setup, the
invitation flow, `bootstrap-admin` and the webhook replay.

Real identity end to end, no mock API: `cmd/clerkmock-serve` is the Clerk twin as a standalone issuer. It serves the JWKS
and the invitations API over HTTPS (self-signed, written to `-ca-out`) and a token mint on a plain loopback port.
`CLERK_*` URLs must be https, and macOS Go ignores `SSL_CERT_FILE`, so run the Linux build of `meterwised` in a container
that trusts the twin's certificate (`SSL_CERT_FILE=/ca.pem`, Postgres and the twin reached as `host.docker.internal`).
Build the web app with its dev-only adapter pointed at the mint, so it signs in with real signed tokens:

```sh
go run ./cmd/clerkmock-serve -ca-out /tmp/ca.pem          # prints the CLERK_* values and MINT_URL as JSON
cd web && VITE_MOCK_TOKEN_URL=http://127.0.0.1:9444/mint NODE_ENV=development npx vite build --mode development --outDir /tmp/dist
```

That bundle contains the mock on purpose; `npm run build:release` fails if `dist` does. The driver used for the Plan 02
acceptance run is kept with its report (`reports/02-e2e/`).

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | required | `postgres://` URL |
| `LISTEN_PROXY` / `LISTEN_API` / `LISTEN_ADMIN` | `:8080` / `:8081` / `127.0.0.1:9090` | must be distinct ports |
| `PROXY_PUBLIC_HOST` / `APP_NAME` | `localhost:8080` / `Meterwise` | returned by `GET /api/config` |
| `PRIVACY_MODE` | `true` | accepted (`true`/`false`) but has no effect: tollgate v0.1.0 has no switch for it, so `false` only logs a boot warning (decision D12 stays unimplemented until tollgate adds one) |
| `MAX_WORKSPACES` / `MAX_CONCURRENT_PER_WORKSPACE` | `20` / `8` | operational limits (D13) |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | base URL of an OTLP/HTTP collector; unset turns tracing off |
| `CLERK_ISSUER` / `CLERK_JWKS_URL` | unset | exact token `iss` and the JWKS URL (https); both required with `APP_ENV=prod` |
| `CLERK_SECRET_KEY` / `CLERK_WEBHOOK_SECRET` | unset | backend API key (invitations) and Svix signing secret (`whsec_...`); required with `APP_ENV=prod`, never printed |
| `CLERK_AUTHORIZED_PARTIES` | unset | origins allowed in the token `azp`; required outside `APP_ENV=dev` (enforced whenever set) |
| `CLERK_API_URL` / `CLERK_PUBLISHABLE_KEY` | `https://api.clerk.com/v1` / unset | backend API base; publishable key (web) |
| `APP_URL` | unset | public URL of the web app; invitation emails land on `APP_URL/accept-invite` |
| `WEB_DIR` | `web/dist` | built web app served on `LISTEN_API` |
| `LOG_LEVEL` / `APP_ENV` | `info` / `prod` | `debug|info|warn|error` / `dev|test|prod`; unset or empty means `prod`. Outside `dev`, `serve` needs the Clerk settings and, until the proxy has a real authenticator, still refuses to boot (the data plane admits every caller as one tenant) |

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
