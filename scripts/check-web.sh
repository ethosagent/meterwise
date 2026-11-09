#!/usr/bin/env bash
# Gate: the web app. In order: install (npm ci, only when node_modules is stale),
# biome check (format + lint, noBarrelFile, noExplicitAny), tsc --noEmit (strict),
# vitest with the 70 % coverage floor (web/vite.config.ts), vite build, then the
# structural rules: no index.ts barrels, every web/src/features/<name>/ has a
# README, and the web dependency law H5 (internal/archtest).
# No web/package.json = explicit SKIP (ruling R5); with one, everything is real.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
if [ ! -f web/package.json ]; then
  # SKIP only when the web app never existed: a deleted package.json is not "no input" (ruling R5)
  if [ -d web/src ] || tracked_on_base web/package.json; then
    echo "✗ web/package.json is missing but the web app exists (web/src, or web/package.json on the base branch)"
    exit 1
  fi
  skip web "no web/package.json yet"
  exit 0
fi
fail=0 errs=()
err() { echo "✗ $*"; fail=1; errs+=("$*"); }

# Every step runs even if an earlier one failed, so one run lists all problems.
cd web
if [ ! -d node_modules ] || [ package-lock.json -nt node_modules/.package-lock.json ]; then
  npm ci --no-audit --no-fund || err "npm ci failed"
fi
for step in check typecheck test build; do
  npm run --silent "$step" || err "npm run $step failed"
done

barrels="$(find src \( -name 'index.ts' -o -name 'index.tsx' -o -name 'index.mts' -o -name 'index.js' -o -name 'index.jsx' -o -name 'index.mjs' \) -not -path '*/node_modules/*' | sort)"
[ -z "$barrels" ] || err "index.ts barrels are banned (grep-friendly names, no re-export chains): $(echo "$barrels" | tr '\n' ' ')"

shopt -s nullglob
features=(src/features/*/)
if [ "${#features[@]}" -eq 0 ]; then
  skip web/features "no web/src/features/<name>/ yet; each one will need a README.md (<= 120 lines)"
fi
for d in ${features[@]+"${features[@]}"}; do
  [ -f "${d}README.md" ] || err "${d} has no README.md (one folder per feature, documented)"
done
cd ..
if ! out="$(go test -count=1 -run 'TestArchitecture$' ./internal/archtest 2>&1)"; then
  echo "$out" | head -n 30
  err "web dependency law H5 (or another archtest rule) failed"
fi
[ "$fail" -eq 0 ] || { echo "--- web gate failures:"; printf '  ✗ %s\n' "${errs[@]}"; }
exit "$fail"
