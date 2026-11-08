#!/usr/bin/env bash
# Gate: go.mod/go.sum are tidy in the root and the tools module. Third-party
# requirements are policed by archtest H6 (allowlist + ADR), not here.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
go mod tidy -diff || { echo "✗ root module is not tidy (fix: go mod tidy)"; exit 1; }
(cd tools && go mod tidy -diff) || { echo "✗ tools module not tidy (fix: cd tools && go mod tidy)"; exit 1; }
