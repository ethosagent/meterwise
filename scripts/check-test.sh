#!/usr/bin/env bash
# Gate: all tests, offline (-tags=offline, -mod=readonly, internal/nonet guard in
# each package's TestMain), with the race detector, -short and a total coverage floor.
# -short skips the wall-clock timing tests (nonet.SkipTiming); check-bench.sh runs them serially.
#   COVERAGE_MIN       minimum total statement coverage, percent (default 75)
#   TEST_DATABASE_URL  loopback Postgres for database tests; when unset and docker
#                      is available the compose Postgres is started (make db-up).
#                      Unset under CI=true makes those tests FAIL; locally they print
#                      "! SKIP test ..." so a green run never hides them.
# Never lower COVERAGE_MIN to go green: add tests (protected path).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
COVERAGE_MIN="${COVERAGE_MIN:-$COVERAGE_MIN_DEFAULT}"
mkdir -p .logs
if [ -z "${TEST_DATABASE_URL:-}" ] && [ "${CI:-}" != "true" ]; then
  if dsn="$(ensure_pg)"; then export TEST_DATABASE_URL="$dsn"; else skip test-db "no Postgres reachable and docker unavailable; database tests will SKIP"; fi
fi
cover_gate
