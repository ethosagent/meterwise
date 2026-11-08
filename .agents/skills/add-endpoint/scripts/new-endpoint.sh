#!/usr/bin/env bash
# new-endpoint.sh METHOD /pattern permission ResponseDTO ["Summary"]
# Appends a row to the route-to-permission table in internal/contract/routes.go
# and regenerates openapi.json and the web contract twin. Use permission "public"
# for an unauthenticated route.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
method="${1:?usage: new-endpoint.sh METHOD /pattern permission ResponseDTO [summary]}"
pattern="${2:?pattern}" perm="${3:?permission or 'public'}" dto="${4:?response DTO name}" summary="${5:-$method $pattern}"
[[ $method =~ ^(GET|POST|PUT|PATCH|DELETE)$ ]] || { echo "✗ method must be GET|POST|PUT|PATCH|DELETE" >&2; exit 1; }
[[ $pattern == /api/* || $pattern == /healthz ]] || { echo "✗ pattern must start with /api/" >&2; exit 1; }
grep -q "reflect.TypeFor\[$dto\]" internal/contract/dto.go || { echo "✗ DTO $dto is not registered in internal/contract/dto.go DTOs()" >&2; exit 1; }
grep -q "Method: \"$method\", Pattern: \"$pattern\"" internal/contract/routes.go && { echo "✗ route already exists" >&2; exit 1; }
if [ "$perm" = public ]; then fields="Public: true"; else fields="Permission: \"$perm\""; fi
row="	{Method: \"$method\", Pattern: \"$pattern\", $fields, Summary: \"$summary\", Response: \"$dto\"},"
awk -v row="$row" '/^var routes = \[\]Route\{/ {inr=1} inr && /^}/ {print row; inr=0} {print}' internal/contract/routes.go >internal/contract/routes.go.tmp
mv internal/contract/routes.go.tmp internal/contract/routes.go
gofmt -w internal/contract/routes.go
go run ./cmd/gen
echo "added $method $pattern ($perm) -> $dto; now write the failing handler test and the handler"
