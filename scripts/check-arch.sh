#!/usr/bin/env bash
# Gate: dependency law D1 P1 H0-H6 (ARCHITECTURE.md §3), enforced by internal/archtest,
# including its seeded-violation tests (a rule that cannot go red fails the gate).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
go test -count=1 ./internal/archtest
