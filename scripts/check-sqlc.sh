#!/usr/bin/env bash
# Gate: sqlc output is generated and current (`sqlc diff`). SKIP only when no sqlc
# input of any shape exists (ruling R5; see sqlc_state in lib.sh). A misnamed config,
# queries or output without sqlc.yaml, or a config without queries FAILS, and so does
# an out-of-date output.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
state="$(sqlc_state)"
case "$state" in
  none)
    skip sqlc "no sqlc input exists yet (Plan 01 adds the store); this gate fails closed the moment one does"
    exit 0 ;;
  error:*) echo "✗ ${state#error: }"; exit 1 ;;
esac
gotool sqlc diff || { echo "✗ sqlc output is stale or hand-edited. Fix: gotool sqlc generate (make sqlc)"; exit 1; }
