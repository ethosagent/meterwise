#!/usr/bin/env bash
# Gate: sqlc output is generated and current (`sqlc diff`), and every query file
# compiles against the migrations (`sqlc vet` is not used: no rules configured).
# No sqlc.yaml yet = explicit SKIP (ruling R5); once it exists the gate is real:
# a missing queries directory or an out-of-date output FAILS.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
if [ ! -f sqlc.yaml ]; then
  skip sqlc "no sqlc.yaml yet (Plan 01 adds the store); this gate fails closed once it exists"
  exit 0
fi
[ -d internal/store/queries ] || { echo "✗ sqlc.yaml exists but internal/store/queries/ does not"; exit 1; }
gotool sqlc diff || { echo "✗ sqlc output is stale or hand-edited. Fix: gotool sqlc generate (make sqlc)"; exit 1; }
