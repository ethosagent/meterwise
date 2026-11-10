# Local development

One page for the owner: set up, run (native or Docker), sign in, create a workspace, send traffic. `make help` lists every target.

## 1. First-time setup

```sh
make setup        # checks go/node/npm/docker, downloads deps, npm ci, installs git hooks, creates .env from .env.example
```

`make setup` is idempotent and says what is missing. `.env` is gitignored and holds dev-only values (ports, a mock Clerk
key); never put a real provider or Clerk key in it. Two ways to run, pick one at a time (they share ports 8080/8081/9090;
override with `APP_API_PORT=...` etc. in `.env`):

| | Native (`make dev`) | Docker (`make up`) |
|---|---|---|
| Runs | Go binary + Vite hot reload; Postgres in Docker | Postgres, migrate, Clerk mock, meterwised in compose profile `app` |
| Sign-in | seeded dev admin, no login screen | the mock login screen, real signed tokens |
| Data | tmpfs Postgres: gone when it stops | named volume `appdb-data`: survives `make down` |

## 2a. Native

```sh
make infra-up     # Postgres 16 on 127.0.0.1:54329 (the shared test database; tmpfs)
make dev          # build, migrate, serve (APP_ENV=dev) + Vite; prints the URLs
make dev-logs     # follow logs; make dev-stop to stop; make infra-down when done
```

Open the printed Vite URL (http://127.0.0.1:5173). With no `CLERK_*` set, `APP_ENV=dev` signs every request in as the seeded
`dev-admin@meterwise.local`, so there is nothing to bootstrap. `make migrate`, `make migrate-status`, `make psql` work against
`DATABASE_URL`. The real Clerk adapter cannot run natively against the twin: `CLERK_*` must be https and macOS Go ignores
`SSL_CERT_FILE`, which is why the Docker stack exists.

## 2b. Docker

```sh
make up           # builds the dev images, starts the stack, creates the first admin, prints the URL
make logs         # SERVICE=meterwised to pick one; make ps; make down (keeps data); make docker-reset (destroys data)
```

Open http://127.0.0.1:8081 and sign in with the mock using `DEV_ADMIN_EMAIL` (default `admin@meterwise.local`). Services:
`appdb` (host 54330), `clerkmock` (HTTPS JWKS + invitations inside the network, token mint on 127.0.0.1:9444), `migrate`
(one-shot), `meterwised` (proxy 8080, API + web 8081, admin 9090; all bound to 127.0.0.1). The web bundle is the dev build
with the mock adapter pointed at the mint; the release image (`make docker-build`, needs `CLERK_PUBLISHABLE_KEY`) never
contains it. `APP_ENV=dev` in the stack because outside dev `serve` needs live Clerk settings. After a rebuild or if the twin
certificate (valid 48 h) expires, `make restart`. Tools: `make docker-migrate`, `docker-psql`, `docker-shell`,
`docker-bootstrap-admin EMAIL=...`.

## 3. First admin and a workspace

Docker: `make up` already invited `DEV_ADMIN_EMAIL`; for another person `make docker-bootstrap-admin EMAIL=you@example.com`
(refused while an admin is active: invite from Settings > Organization), then sign in with that email. Native has the seeded
admin; `make bootstrap-admin EMAIL=...` only records an invitation. See [`clerk.md`](clerk.md) for the rest.

In the web app: Settings > Workspaces > create one, then open its Setup page and copy the proxy URL (the same JSON is
`GET /api/workspaces/{id}/setup`). Agents use that URL as their base URL; they keep their own provider key.

## 4. Send traffic

```sh
make demo-traffic           # native DB          make docker-demo-traffic   # the Docker stack's DB
export ANTHROPIC_BASE_URL="<proxy URL from Setup>"      # real agent, your own key; OpenAI: OPENAI_BASE_URL
```

Dev without a workspace URL: the proxy accepts one tenant at `http://127.0.0.1:8080/openai/v1` (or `/anthropic`).

## 5. Read it back

```sh
API=http://127.0.0.1:8081; WS=<workspace id>
# Docker: mint a token (native needs no header at all)
TOKEN=$(curl -s "http://127.0.0.1:9444/mint?email=admin@meterwise.local" | jq -r .token); H="Authorization: Bearer $TOKEN"
curl -s -H "$H" "$API/api/workspaces" | jq '.items[0].id'                       # the workspace id
curl -s -H "$H" "$API/api/workspaces/$WS/sessions" | jq                          # session list
curl -s -H "$H" "$API/api/sessions/<sessionId>?workspaceId=$WS" | jq             # one session
curl -s -H "$H" "$API/api/workspaces/$WS/stats" | jq                             # totals (spend, calls, sessions, by day)
```

## Troubleshooting

| Symptom | Fix |
|---|---|
| `port is already allocated` / `invalid hostPort` | another stack or the native run holds it: `make dev-stop` / `make down`, or set `APP_*_PORT` in `.env` |
| `make up` stalls on healthz | `make logs SERVICE=meterwised`; a TLS error to `clerkmock` means the twin restarted: `make restart` |
| Mock login says not invited (403) | only `DEV_ADMIN_EMAIL` and invited emails sign in; `make docker-bootstrap-admin EMAIL=...` |
| Mock login fails with a network error | the mint port changed after the build: `make rebuild` (it is baked into the bundle) |
| `docker-build` asks for a key | set `CLERK_PUBLISHABLE_KEY` (public key of your Clerk instance) |
| Fresh start | `make down && make docker-reset && make up`; native: `make infra-reset` |
| Gate fails on `migrate` or database tests | `make infra-up`, then `export TEST_DATABASE_URL=...54329/meterwise?sslmode=disable` |

## Configuration (environment)

`go run ./cmd/meterwised config check -json` lists every problem at once.

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
