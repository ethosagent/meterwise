# workspace

**Settings / Workspace** (`/settings/workspace`) and the create-workspace dialog.

- Shows: the active workspace's name (admins can rename), its proxy URL with a Copy button, and a
  danger zone with delete. The default workspace cannot be deleted: the button is disabled and says why.
  There is no Reset URL here (Plan 04 adds it).
- `CreateWorkspaceDialog` (opened from the sidebar switcher): name in, proxy URL with Copy out.
  A `409 workspace_limit` shows "Workspace limit reached"; `422` shows the server's first problem.
- Endpoints: `GET|POST /api/workspaces`, `PATCH|DELETE /api/workspaces/{id}` (hooks in `api/identity.ts`).
- Permission: `workspaces.manage` gates rename, delete and create; members see the name and URL, read-only.
- Tests: `Page.test.tsx` runs against an in-memory fake API at the fetch layer (`test/mockApi.ts`).
