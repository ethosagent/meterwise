#!/usr/bin/env bash
# Gate: generated files are generated. Each is regenerated in memory and compared
# with the file on disk, so a hand edit or a stale file fails:
#   web/src/contract/generated.ts   contract TS twin   (cmd/gen contract-ts)
#   internal/api/openapi.json       OpenAPI            (cmd/gen openapi)
#   docs/schema.md                  schema doc         (cmd/gen schema)
#   internal/store/sqlc/**          sqlc output        (sqlc diff, once sqlc.yaml exists)
#   web/src/**/taxonomy.generated.ts  RBAC taxonomy twin (from Plan 03; SKIP until then)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
go run ./cmd/gen -check
if [ -f sqlc.yaml ]; then
  gotool sqlc diff || { echo "✗ sqlc output is stale or hand-edited. Fix: gotool sqlc generate"; exit 1; }
else
  skip generated/sqlc "no sqlc.yaml yet"
fi
if [ -f internal/contract/taxonomy.go ]; then
  go run ./cmd/gen -check taxonomy
else
  skip generated/taxonomy "no internal/contract/taxonomy.go yet (Plan 03 adds the RBAC taxonomy twin)"
fi
