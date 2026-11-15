# Meterwise

Meterwise is an LLM proxy for coding agents. It cuts the tokens agents waste and shows exactly where every dollar went.

Point your agent's base URL at a Meterwise workspace and keep using your own provider key. Meterwise sits in front of
the provider, watches each session, stops the calls that are pure waste, and reports two separate numbers: what it
**saved** (certain, traceable to single calls) and what it **likely saved** (estimated). The two are never added together.

It is one Go binary with a React dashboard and Postgres as the only store. It embeds the
[tollgate](https://github.com/ethosagent/tollgate) proxy library.

## What it does

| Feature | What happens | Modes |
|---|---|---|
| **Proxy** | Forwards OpenAI, Anthropic and OpenRouter calls, streamed or not, from `/w/{workspace}/{provider}/...`. Your `Authorization`/`x-api-key` header passes through and is never stored or logged. | always on |
| **Ledger** | Records every call: model, tokens, cost, latency, session. One writer per table, idempotent on `call_id`. | always on |
| **Monitors** | Six waste checks per session: `duplicate_tool_call`, `repeated_read`, `no_progress`, `step_budget`, `reasoning_loop`, `second_guessing`. | `observe`, `note` (adds a hint for the agent), `block` (the proxy answers in place of the provider) |
| **Trim** | Shrinks tool results before they reach the model: repeated lines, long logs, duplicate lines, JSON tables, and (aggressive only) schemas. The result is deterministic, so provider prompt caches stay warm. | `off`, `observe`, `lossless`, `aggressive` |
| **Router** | Sends simple steps to a cheaper model from the same provider, on your own key. If a monitor flags a routed call, the next calls go back to the strong model. | `off`, `observe`, `conservative`, `aggressive` |
| **Memory** | When a session ends with a solution, stores a short, redacted trace (pgvector). A later session that looks similar gets one `[proxy] memory:` hint. Only runs where privacy settings allow text. | `off`, `observe`, `hint` |
| **Dashboard** | Overview, savings (saved and likely saved), sessions with every call, monitors, and per-workspace settings. | web UI |
| **Tenancy** | One organization, admin and member roles, invitations, several workspaces with their own URL and settings. Sign-in uses Clerk, with a built-in mock for local runs. | always on |

Privacy mode is **on** by default: only metadata is kept unless a workspace opts in to storing text.

## Quick start (Docker, about 5 minutes)

You need Git, Docker Desktop, Go (the toolchain named in `go.mod`, currently 1.27) and Node 22 or later.

```sh
git clone https://github.com/ethosagent/meterwise.git
cd meterwise
make setup     # checks the tools, downloads deps, installs git hooks, writes .env with dev-only values
make up        # Postgres, migrations, mock sign-in and meterwised; prints the URLs when ready
```

1. Open http://127.0.0.1:8081 and sign in as `admin@meterwise.local`. This is the mock sign-in; no Clerk account is needed.
2. Go to **Settings > Workspaces**, create a workspace and open its **Setup** page.
3. Copy the proxy URL. Locally the stack serves plain HTTP, so use `http://127.0.0.1:8080/w/<publicId>`.

Stop with `make down` (data is kept) or wipe everything with `make docker-reset`. `make help` lists every target.

Want to see it work without a real agent? `make docker-demo-traffic TRIM=lossless ROUTER=conservative MEMORY=hint`
replays looping agents against a local fake provider (offline, no key, no cost). Then reload the dashboard.

## Use it with your agent

Use the workspace URL plus the provider name as the base URL. Keep your own API key in the agent as usual.

| Agent | Setting |
|---|---|
| Claude Code | `export ANTHROPIC_BASE_URL=http://127.0.0.1:8080/w/<publicId>/anthropic` |
| OpenAI SDKs, Codex-style tools | `export OPENAI_BASE_URL=http://127.0.0.1:8080/w/<publicId>/openai/v1` |
| opencode | `provider.<name>.options.baseURL` in `opencode.json`, as below |

opencode (`~/.config/opencode/opencode.json` or `opencode.json` in your project):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "anthropic": { "options": { "baseURL": "http://127.0.0.1:8080/w/<publicId>/anthropic/v1" } }
  }
}
```

If opencode gets a 404, try the URL without the trailing `/v1`. Then run `opencode run "say hi"` and check that the
call appears under **Sessions**.

> Calls you send through the proxy reach the real provider and are billed to your key. Only the demo traffic is offline.

### From another laptop

Every port is bound to `127.0.0.1` on purpose: the dev stack lets any caller use the proxy. Either run the quick start
on that laptop, or tunnel to the machine that runs Meterwise (enable Remote Login on it first):

```sh
ssh -N -L 8080:127.0.0.1:8080 -L 8081:127.0.0.1:8081 -L 9444:127.0.0.1:9444 you@meterwise-host.local
```

The agent settings above then work unchanged. Never bind the dev stack to `0.0.0.0`.

## Run natively (no app containers)

```sh
make infra-up   # Postgres 16 with pgvector on 127.0.0.1:54329 (tmpfs: data is gone when it stops)
make dev        # builds, migrates, runs meterwised (APP_ENV=dev) and the Vite dev server; prints the URLs
make dev-stop
```

Natively there is no sign-in screen: `APP_ENV=dev` signs every request in as the seeded `dev-admin@meterwise.local`.

## Ports

| Port | Service |
|---|---|
| 8080 | Proxy (agents talk to this) |
| 8081 | Web dashboard and control-plane API (`/api/...`) |
| 9090 | Admin: health and `/statusz` |
| 9444 | Mock sign-in token mint (Docker stack only) |
| 54329 / 54330 | Postgres: shared test database / Docker stack database |

## Configuration

All settings come from the environment. `go run ./cmd/meterwised config check -json` lists every problem at once.

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | required | Postgres URL |
| `LISTEN_PROXY` / `LISTEN_API` / `LISTEN_ADMIN` | `:8080` / `:8081` / `127.0.0.1:9090` | listen addresses |
| `APP_ENV` | `prod` | `dev`, `test` or `prod`; outside `dev` the Clerk settings are required |
| `PROXY_PUBLIC_HOST` | `localhost:8080` | host shown in workspace URLs |
| `CLERK_*` | unset | real sign-in (issuer, JWKS, keys, authorized parties) |
| `MAX_WORKSPACES` / `MAX_CONCURRENT_PER_WORKSPACE` | `20` / `8` | limits |
| `HISTORY_MAX_BYTES` | 256 MiB | memory budget for per-session history |
| `MEMORY_*` | unset | embedding/distil model key for the memory feature; memory is off without it |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | unset | OTLP/HTTP collector; unset turns tracing off |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

The full table is in the [local development runbook](docs/runbooks/local-dev.md#configuration-environment).

## Commands

```sh
go run ./cmd/meterwised serve            # proxy, API and web
go run ./cmd/meterwised migrate          # apply migrations (also: migrate status)
go run ./cmd/meterwised bootstrap-admin --email you@example.com
go run ./cmd/meterwised version -json    # every subcommand takes -json
```

API example (Docker stack): mint a mock token, then read the workspaces.

```sh
TOKEN=$(curl -s "http://127.0.0.1:9444/mint?email=admin@meterwise.local" | jq -r .token)
curl -s -X POST -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8081/api/auth/complete-signup   # once per token
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8081/api/workspaces | jq   # 200 with {"items": [...]}
```

Before sign-up is completed, these calls answer `403 not_invited`. Without a token they answer `401`.

## Repository layout

| Path | What it holds |
|---|---|
| `cmd/meterwised` | the binary and its subcommands |
| `cmd/gen`, `cmd/demo-traffic`, `cmd/clerk-replay` | generators, offline demo traffic, webhook replay |
| `internal/contract` | shared types, error codes, the route-to-permission table, `MicroUSD` money |
| `internal/api`, `internal/dataplane` | control-plane API; the embedded tollgate proxy |
| `internal/{ledger,history,monitors,savings,trim,router,memory}` | the features above |
| `internal/{identity,tenancy,access,wsauth,audit}` | sign-in, org and workspaces, permissions, audit log |
| `internal/store` | Postgres pool, migrations (goose), queries (sqlc) |
| `web/` | React, Ant Design and TanStack Query dashboard |
| `docs/` | runbooks, decisions (ADRs), schema |

## Development

```sh
scripts/run-checks.sh          # the full gate; CI and the pre-push hook run exactly this
scripts/run-checks.sh --fast   # fmt vet arch docs size sqlc, in seconds
scripts/quiet.sh go test -race ./internal/config   # any command, failures only
go run ./cmd/gen               # regenerate the TypeScript types, openapi.json and docs/schema.md
```

The gates are `fmt vet mod arch lint test bench vuln docs size sqlc migrate web generated`. Database tests need
`make infra-up` and `TEST_DATABASE_URL=postgres://meterwise:meterwise@127.0.0.1:54329/meterwise?sslmode=disable`.
Tests are offline: only loopback, with mock twins for Clerk and the providers. Run `bench` on a quiet machine,
because a high load average fails its timing budgets.

Rules for contributors live in [CLAUDE.md](CLAUDE.md) (the map), [ARCHITECTURE.md](ARCHITECTURE.md) (layers and the
dependency law) and [docs/decisions](docs/decisions/README.md). The memory feature has its own
[runbook](docs/runbooks/memory.md). Releases: [docs/runbooks/release.md](docs/runbooks/release.md).

## Security

- Provider keys pass through and are never stored or logged.
- The workspace public id in the URL is the proxy credential: treat the URL like a secret.
- The dev stack (`APP_ENV=dev`) has no proxy authentication and binds to localhost only. Do not expose it.
- Every query is scoped by workspace and organization; every API route has a permission row.

## License

Licensed under the [Apache License, Version 2.0](LICENSE). Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied. See the License for the specific language governing permissions and limitations.
