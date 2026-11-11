#!/usr/bin/env bash
# Gate: the wall-clock timing tests (p99/p95 and "took less than N ms" budgets), run
# alone: one package at a time (-p 1), one test at a time (-parallel 1), no race
# detector, no -short. The parallel, race-instrumented `test` gate runs with -short,
# where every one of these skips (internal/nonet SkipTiming); machine load there made
# the budgets fail on noise. Budgets are unchanged. Still offline (-tags=offline,
# -mod=readonly, internal/nonet guard in each package's TestMain).
#   TEST_DATABASE_URL  as in check-test.sh: when unset and docker is available the compose
#                      Postgres is started; otherwise the database timing tests print
#                      "! SKIP ..." locally and FAIL under CI=true.
# The test list is derived: every test that calls nonet.SkipTiming(t). A listed test that
# does not appear in the output (renamed, deleted) fails the gate, so it cannot go quiet.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
mkdir -p .logs
if [ -z "${TEST_DATABASE_URL:-}" ] && [ "${CI:-}" != "true" ]; then
  if dsn="$(ensure_pg)"; then export TEST_DATABASE_URL="$dsn"; else skip bench-db "no Postgres reachable and docker unavailable; database timing tests will SKIP"; fi
fi

files="$(grep -rl --include='*_test.go' 'nonet\.SkipTiming(t)' internal || true)"
[ -n "$files" ] || { echo "✗ bench: no test calls nonet.SkipTiming(t); the timing tests are gone, not passing."; exit 1; }
names=() pkgs=()
while IFS= read -r f; do
  pkgs+=("./$(dirname "$f")")
  while IFS= read -r n; do names+=("$n"); done < <(awk '/^func Test/ { n=$2; sub(/\(.*/, "", n) } /nonet\.SkipTiming\(t\)/ { print n }' "$f")
done <<<"$files"
u=() && while IFS= read -r l; do u+=("$l"); done < <(printf '%s\n' "${pkgs[@]}" | sort -u) && pkgs=("${u[@]}")
u=() && while IFS= read -r l; do u+=("$l"); done < <(printf '%s\n' "${names[@]}" | sort -u) && names=("${u[@]}")
re="^($(IFS='|'; echo "${names[*]}"))\$"

vlog="$REPO_ROOT/.logs/bench-verbose.log" rc=0
echo "bench: $(uptime | sed 's/.*load averages*: *//') load; ${#names[@]} tests in ${#pkgs[@]} packages"
GOFLAGS=-mod=readonly go test -tags=offline -count=1 -p 1 -parallel 1 -v -run "$re" "${pkgs[@]}" >"$vlog" 2>&1 || rc=$?
{ grep -E -- '^(--- FAIL|FAIL|panic:)|^\s+[a-z_]+\.go:[0-9]+' "$vlog" || true; } | head -n 80
awk '/^[[:space:]]*--- SKIP/ { name=$3; getline; sub(/^[[:space:]]+/, ""); print "! SKIP test " name ": " $0 }' "$vlog"
[ "$rc" -eq 0 ] || exit 1
for n in "${names[@]}"; do
  grep -qE -- "^--- (PASS|SKIP): $n " "$vlog" || { echo "✗ bench: $n did not run (renamed or filtered out); the gate must run every SkipTiming test."; exit 1; }
done
echo "bench: ${#names[@]} timing tests ran serially, budgets unchanged"
