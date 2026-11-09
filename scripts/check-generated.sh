#!/usr/bin/env bash
# Gate: generated files are generated. Each is regenerated in memory and compared
# with the file on disk, so a hand edit or a stale file fails (sqlc output: see check-sqlc.sh):
#   web/src/contract/generated.ts   contract TS twin   (cmd/gen contract-ts)
#   internal/api/openapi.json       OpenAPI            (cmd/gen openapi)
#   docs/schema.md                  schema doc         (cmd/gen schema)
#   web/src/**/taxonomy.generated.ts  RBAC taxonomy twin (from Plan 03; SKIP until then)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
go run ./cmd/gen -check
# any file that looks like the RBAC taxonomy (Go or TS, any name) makes this branch real
if [ -n "$(list_files | grep -iE 'taxonomy' | grep -v -E '\.md$|^(docs/|\.agents/|\.claude/|scripts/|internal/archtest/|cmd/gen/)' | head -n 1)" ]; then
  go run ./cmd/gen -check taxonomy
else
  skip generated/taxonomy "no taxonomy source exists yet (Plan 03 adds the RBAC taxonomy twin)"
fi
