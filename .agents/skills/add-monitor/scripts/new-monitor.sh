#!/usr/bin/env bash
# new-monitor.sh <name> — create positive/negative ring fixtures for a monitor and print the
# remaining checklist. Needs internal/monitors (created by the monitors plan).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
name="${1:?usage: new-monitor.sh <name>   (snake_case)}"
[[ $name =~ ^[a-z][a-z0-9_]*$ ]] || { echo "✗ name must be snake_case" >&2; exit 1; }
[ -d internal/monitors ] || { echo "✗ internal/monitors does not exist yet; the monitors plan creates it" >&2; exit 1; }
dir="internal/monitors/testdata/$name"
[ ! -e "$dir" ] || { echo "✗ $dir exists" >&2; exit 1; }
mkdir -p "$dir"
printf '{\n  "description": "ring that %s must flag",\n  "ring": [],\n  "expect": {"fires": true}\n}\n' "$name" >"$dir/positive.json"
printf '{\n  "description": "ring that %s must leave alone",\n  "ring": [],\n  "expect": {"fires": false}\n}\n' "$name" >"$dir/negative.json"
cat <<LIST
created $dir/{positive,negative}.json (fill "ring" with synthetic calls, no real prompts)
next: 1 failing table test  2 internal/monitors/$name.go  3 savings rule  4 settings default  5 dashboard row  6 benchmark
LIST
