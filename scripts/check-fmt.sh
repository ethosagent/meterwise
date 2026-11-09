#!/usr/bin/env bash
# Gate: every tracked or new Go file is gofmt-formatted AND parses (a syntax error is a
# failure, not silence). Fix: gofmt -w .
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
files="$(list_files '*.go' | grep -v '^tools/' || true)"
[ -n "$files" ] || exit 0
out="$(printf '%s\n' "$files" | tr '\n' '\0' | xargs -0 gofmt -l 2>&1)"
rc=$?
if [ "$rc" -ne 0 ] || [ -n "$out" ]; then
  echo "✗ not gofmt-formatted or does not parse (fix: gofmt -w .):"
  echo "$out"
  exit 1
fi
