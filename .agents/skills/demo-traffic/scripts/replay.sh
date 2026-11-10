#!/usr/bin/env bash
# replay.sh - fill a workspace's ledger with synthetic sessions through cmd/demo-traffic.
#   scripts/replay.sh                     5 sessions into the default workspace (DATABASE_URL)
#   ROUNDS=20 WORKSPACE=<uuid> scripts/replay.sh
# cmd/demo-traffic runs a real tollgate proxy with the ledger hook against a loopback fake upstream:
# no provider key, no live provider, no running meterwised needed. Docker stack: make docker-demo-traffic.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
[ -n "${DATABASE_URL:-}" ] || { echo "set DATABASE_URL (make db-up && go run ./cmd/meterwised migrate)" >&2; exit 2; }
args=(-rounds "${ROUNDS:-5}")
[ -z "${WORKSPACE:-}" ] || args+=(-workspace "$WORKSPACE")
exec go run ./cmd/demo-traffic "${args[@]}"
