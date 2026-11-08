#!/usr/bin/env bash
# Gate: govulncheck (pinned in tools/go.mod) against the module's call graph.
# It queries the vulnerability database, so it needs network access (CI and
# local dev); the offline guarantee applies to tests, not to this gate.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
gotool govulncheck ./...
