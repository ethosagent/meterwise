# 0015. RBAC kernel: grants, one gateway, last-admin lock

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** plan/03-access-management.md, 03-interface.md

## Context

Plan 02 decided access with `contract.Allows(role, route)`, an inline role table. Plan 03 needs
per-workspace grants through groups and org defaults. `contract` imports stdlib only (H0), so it
cannot import the taxonomy that lives in `access`. Each handler checking roles itself would let
the rule drift; two simultaneous demotions could also leave an org with no admin.

## Decision

1. **Taxonomy** lives in `internal/access/taxonomy.go` (resource types x actions, scope workspace
   or org). `web/src/access/taxonomy.ts` is generated from it inside the `generated` gate.
   `contract` holds route permissions as string constants; an `access` test proves every route
   permission is in the taxonomy.
2. **Three route classes**: a taxonomy permission, `PermMember` (any active member), `PermAdmin`.
   `PermSelf` marks self routes whose handler scopes to the caller. Admin bypasses every grant.
   A member's decision is the union of group grants and org defaults; additive, no deny.
3. **One gateway** (`internal/api/authz.go`): resolve session, `not_invited` for a non-member,
   workspace-in-org check (404), then a pure `PermissionEvaluator` over one `GrantRepository`
   read (403 with `details.reason`), then the handler. Handlers go through `register`/`adapt`.
4. **`Route.WorkspaceParam`** names the path parameter holding the workspace id (`"id"` today).
   Plan 05 extends it for `/api/sessions/{id}` by adding a resolver from the session id to its
   workspace before the same check, leaving the field's meaning unchanged.
5. **Migrations** (one feature per file, so no `migration-families.txt` entry): `0007`
   resource_permissions, `0008` groups (group_* tables share the stem), `0009`
   organization_permissions with its own `workspace_id`, `resource_type`, `action` columns and the
   seed of `read` on the five workspace-scoped resources.
6. **Last admin**: demote and remove take `pg_advisory_xact_lock(hashtext('admins:'||org))`, count
   active admins inside the transaction, and answer `409 last_admin`. Adding to a group takes
   `FOR SHARE` on the membership row so a concurrent removal cannot leave a stale group row.
7. **H11** forbids inline role checks outside `contract`, `access`, `store`.

## Consequences

Easier: a new route is one table row; access logic is one pure function with table tests.
Harder: a route without a permission row fails the contract test. Removing a member deletes their
group rows and revokes open invitations for that email; authorization never reads `users.email`.
Known gap: the Clerk `user.deleted` webhook soft-deletes a user without the last-admin check.
Enforced by: `TestEveryProtectedRouteIsBehindTheGateway`, `TestConcurrentDemotionLeavesExactlyOneAdmin`,
archtest H11 (seeded red case, selftest row `H11`), `TestTaxonomyTwinIsGeneratedFormattedAndChecked`.
