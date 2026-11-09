#!/usr/bin/env bash
# Gate: golangci-lint, version pinned in tools/go.mod, config in .golangci.yml.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
# --allow-parallel-runners: parallel worktrees and CI jobs lint at the same time on one machine
gotool golangci-lint run --allow-parallel-runners --config "$REPO_ROOT/.golangci.yml" ./...
