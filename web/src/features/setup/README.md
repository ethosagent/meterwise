# setup

**Setup** (`/setup`): the workspace URL is the whole onboarding.

- Shows: the active workspace's proxy URL (mono, Copy, Reset URL), tabs with the five client snippets
  (OpenAI SDK, Anthropic SDK, OpenCode, Claude Code, curl; each copyable, URL filled in server-side) with the
  note "keep using your own provider key", and a Status line: "waiting for first call…" or
  "connected · last call <relative> · N calls today" (polled every 15 s).
- Endpoint: `GET /api/workspaces/{id}/setup` (`api/setup.ts`). Types in `api/contract04.ts` until the generated
  contract carries them.
- Permission: `workspace_url:read` on the active workspace. Without it (or when the API sends `proxyUrl: null`)
  the page says the URL is hidden and to ask an admin, and never calls the setup endpoint. Reset URL needs
  `workspace_url:manage`. Gating is `usePermission` only, never a role.
- Reset URL and the proxy URL row are shared with Settings > Workspace (`features/workspace/WorkspaceUrl.tsx`).
  A reset refreshes setup, the workspace list and the audit log.
- Tests: `Page.test.tsx` against the in-memory fake API (`test/mockApi.ts`, URL routes in `test/mockUrl.ts`).
