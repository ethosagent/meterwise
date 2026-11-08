#!/usr/bin/env bash
# Gate: go vet on every package, with the offline tag the tests use.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
go vet -tags=offline ./...
