#!/usr/bin/env bash
# make setup: verify the toolchain, fetch dependencies, install hooks, create .env. Idempotent.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."

bad=0
need() { # need <command> <install hint>
  if ! command -v "$1" >/dev/null 2>&1; then echo "  MISSING  $1: $2"; bad=1; return 1; fi
}
echo "==> checking tools"
need go "install Go (version in go.mod): https://go.dev/dl/" && echo "  ok       $(go version)"
if need node "install Node 22 or newer: https://nodejs.org/"; then
  major=$(node -p 'process.versions.node.split(".")[0]')
  if [ "$major" -lt 22 ]; then echo "  TOO OLD  node $(node -v): need 22 or newer"; bad=1; else echo "  ok       node $(node -v)"; fi
fi
need npm "ships with Node" && echo "  ok       npm $(npm -v)"
if need docker "install Docker Desktop or Docker Engine: https://docs.docker.com/get-docker/"; then
  if docker info >/dev/null 2>&1; then echo "  ok       $(docker --version)"
  else echo "  NOT UP   docker is installed but the daemon is not reachable: start Docker Desktop"; bad=1; fi
  docker compose version >/dev/null 2>&1 || { echo "  MISSING  docker compose v2 plugin"; bad=1; }
fi
command -v psql >/dev/null 2>&1 && echo "  ok       psql" || echo "  note     psql not found: make psql falls back to the postgres container"
command -v jq >/dev/null 2>&1 && echo "  ok       jq" || echo "  note     jq not found: the curl examples in docs/runbooks/local-dev.md use it"
if [ "$bad" -ne 0 ]; then echo "setup: fix the items above and run make setup again" >&2; exit 1; fi

echo "==> go modules"
go mod download
(cd tools && go mod download)
echo "==> web dependencies (npm ci)"
(cd web && npm ci --no-audit --no-fund)
echo "==> git hooks"
scripts/install-hooks.sh
echo "==> .env"
if [ -f .env ]; then echo "  .env exists, left as is"; else cp .env.example .env && echo "  created .env from .env.example"; fi
echo "setup done. Next: make up (everything in Docker) or make infra-up && make dev (native). make help lists all targets."
