---
name: add-endpoint
description: Add a control-plane API endpoint end to end: route-to-permission row, DTO, handler, sqlc query, handler test, OpenAPI and the web client type. Use for any new /api route.
---

# Add an endpoint

Rules first:
- **The route table is the source.** Add the row in `internal/contract/routes.go` first; the gateway,
  `openapi.json` and `web/src/contract/generated.ts` are generated from it (gate `generated`).
- **No inline role checks** in handlers; name a permission in the row (TNT-5). Non-public rows need one.
- Errors come from the registry in `internal/contract/errors.go` (`{code,message,details}`).
- Every query filters by `workspace_id` or `org_id` (TNT-1); cross-tenant ids answer `not_found` (TNT-6).
- Money is a decimal string on the wire, `contract.MicroUSD` in Go.

## Steps

1. Add the response DTO to `internal/contract/dto.go` (and to `DTOs()`).
2. `.agents/skills/add-endpoint/scripts/new-endpoint.sh GET /api/v1/things thing:read ThingList`
   appends the row and regenerates `openapi.json` and the web twin.
3. Write the failing handler test first, then the handler in `internal/api/`, then the sqlc query
   (`add-migration` if a table is missing).
4. Use the generated TS type in `web/src/features/<name>/api.ts`; never hand-write a DTO twin.
5. `scripts/run-checks.sh` and review with the `tenancy-authz` lens.
