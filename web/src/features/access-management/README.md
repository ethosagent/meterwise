# access-management

**Settings / Access management**, route `/settings/access`. Plan 03.

- Tabs: **Groups** (table; a row opens the group Drawer), **Users** (read-only, with each user's groups),
  **Invitations** (the shared `organization/InvitationsTable`).
- Group Drawer: rename, attach or detach workspaces, a **Permissions** matrix (resourceType x action, one table per
  attached workspace plus an Organization table for org-scoped resources, saved with one PUT), a **Members** transfer
  list, delete.
- Endpoints: `GET|POST /api/groups`, `GET|PATCH|DELETE /api/groups/{id}`, `GET|PUT .../permissions`,
  `POST|DELETE .../workspaces[/{wsId}]`, `GET|POST .../members`, `DELETE .../members/{userId}`, `GET /api/members`,
  `GET|POST|DELETE /api/invitations...`. Hooks live in `src/api/access.ts` (shared with the Organization page).
- Permissions: Groups and Users tabs need `member:read`; Invitations needs `member:manage`; every write in the Drawer
  and the New group button are admin-only (`useIsAdmin`, the gateway's PermAdmin) and are hidden or disabled otherwise.
  With neither read nor manage the page says so and calls nothing.
- Errors: `group_name_taken`, `last_admin` and `already_member` map to readable lines through `describeApiError`.
- Test: `Page.test.tsx` (mock server in `src/test/mockAccess.ts`); grant semantics in `src/access/access.test.tsx`.

`src/access/plan03-types.ts` holds temporary types and a hand-written taxonomy until the Go half's generated
`contract/generated.ts` and `access/taxonomy.ts` land; the integration step deletes it.
