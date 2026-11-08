---
paths:
  - "internal/api/**"
  - "internal/contract/**"
---

# Control-plane API rules

Checklist: `.agents/reviews/tenancy-authz.md` (TNT-*). Use the `add-endpoint` skill.

- The **route-to-permission table** (`internal/contract/routes.go`) is the source. A handler without a row is a bug.
- No inline role checks in handlers; the gateway middleware and the evaluator decide (TNT-5).
- Errors are `{code, message, details}` from the registry in `contract/errors.go`; use `contract.AsError` so internals never leak.
- Every workspace id from a URL is resolved for the calling principal; other tenants' ids answer `not_found` (TNT-3, TNT-6).
- Money crosses the wire as a decimal string (`contract.MicroUSD`), never a JSON number.
- `internal/contract` imports the standard library and the tollgate root only (H0).
- After a DTO or route change run `go run ./cmd/gen` and commit `openapi.json` and `web/src/contract/generated.ts`.
