#!/usr/bin/env bash
# The single gate. The pre-push hook, CI and `make check` all run THIS script,
# so local == hook == CI by construction. It runs every gate, reports ALL
# failures (not fail-fast), prints only failures, and keeps full logs in .logs/.
#
#   scripts/run-checks.sh            every gate
#   scripts/run-checks.sh fmt test   only the named gates
#   scripts/run-checks.sh --list     print gate names
#   scripts/run-checks.sh --fast     the seconds-cheap gates (FAST_GATES in scripts/lib.sh)
#   scripts/run-checks.sh [--json] [--fast] [gate...]  flags in any order; --json adds one JSON
#                                    summary line on stdout: {"ok":bool,"gates":[{"name","ok","secs","log"}]}
#                                    (named gates win over --fast; an unknown flag fails)
#
# Add a gate: drop scripts/check-<name>.sh and add <name> to GATES. A gate whose inputs do not
# exist yet prints "! SKIP <gate>: <reason>" (ruling R5) and goes real, failing closed, once they do.
# This file is a protected path (.agents/risk.yaml): never remove a gate to go green.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

. scripts/lib.sh
GATES=(fmt vet mod arch lint test vuln docs size sqlc migrate web generated)
JSON=0
while [ "$#" -gt 0 ]; do
  case "$1" in
    --list) printf '%s\n' "${GATES[@]}"; exit 0 ;;
    --json) JSON=1 ;;
    --fast) GATES=("${FAST_GATES[@]}") ;;
    -*) echo "✗ unknown flag $1 (flags: --list --json --fast)" >&2; exit 2 ;;
    *) break ;;
  esac
  shift
done
if [ "$#" -gt 0 ]; then GATES=("$@"); fi

mkdir -p .logs
failures=() results=()
for g in "${GATES[@]}"; do
  script="scripts/check-$g.sh"
  if [ ! -f "$script" ]; then echo "✗ $g: no such gate ($script)"; failures+=("$g"); continue; fi
  t0=$SECONDS
  if bash "$script" >".logs/$g.log" 2>&1; then ok=true; else ok=false; failures+=("$g"); fi
  secs=$((SECONDS - t0))
  if $ok; then
    echo "✓ $g (${secs}s)"
    grep -h '^! ' ".logs/$g.log" | sed 's/^/    /' # soft warnings stay visible
  else
    echo "✗ $g (${secs}s) — last 40 lines of .logs/$g.log:"
    tail -n 40 ".logs/$g.log" | sed 's/^/    /'
  fi
  results+=("{\"name\":\"$g\",\"ok\":$ok,\"secs\":$secs,\"log\":\".logs/$g.log\"}")
done

echo "--- ${SECONDS}s"
if [ ${#failures[@]} -ne 0 ]; then
  echo "✗ failed: ${failures[*]}  (full logs: .logs/<gate>.log)"
  rc=1 all_ok=false
else
  echo "✓ all gates passed"
  rc=0 all_ok=true
fi
# the JSON summary is the last line of stdout
[ "$JSON" -eq 0 ] || printf '{"ok":%s,"gates":[%s]}\n' "$all_ok" "$(IFS=,; echo "${results[*]}")"
exit "$rc"
