# workspace

**Settings / Workspace** (`/settings/workspace`) and the create-workspace dialog.

- Shows: the active workspace's name (admins can rename), its proxy URL with Copy and Reset URL
  (`WorkspaceUrl.tsx`, shared with Setup), the Allowed client CIDRs tags editor (`AllowedCidrs.tsx`) and a
  danger zone with delete. The default workspace cannot be deleted: the button is disabled and says why.
- The URL is gated: `publicId`, `proxyUrl` and `allowedClientCidrs` are null without `workspace_url:read`
  (the row says the URL is hidden). Reset URL and the CIDR editor need `workspace_url:manage`
  (`PUT /api/workspaces/{id}/url/allowed-cidrs`; the server validates, a 422 shows its first problem).
- `CreateWorkspaceDialog` (opened from the sidebar switcher): name in, proxy URL with Copy out.
  A `409 workspace_limit` shows "Workspace limit reached"; `422` shows the server's first problem.
- Endpoints: `GET|POST /api/workspaces`, `PATCH|DELETE /api/workspaces/{id}` (hooks in `api/identity.ts`).
- Permissions: `workspaces.create` is open to any member; `workspaces.manage` (rename, delete) is admin only, so a member sees the name and URL read-only. Mirrors `internal/contract/routes.go`; Plan 03 replaces both with grants.
- The active workspace is never substituted: if `Me.activeWorkspaceId` is not in the list, both are refetched once and an error state shows (the URL is the workspace).
- Tests: `Page.test.tsx` runs against an in-memory fake API at the fetch layer (`test/mockApi.ts`).
