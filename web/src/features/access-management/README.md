# access-management

Stub page for **Settings / Access management**, route `/settings/access`.

- Shows: the page head (title and one-line description from `shell/nav.tsx`) and an `Empty` body.
- Endpoints: none yet.
- Permission: none yet; real gating with `hasPermission` arrives with auth (Plan 02).
- Test: `Page.test.tsx` renders the page at its route and checks the head and the empty body.

When the real page lands, replace `StubPage` in `Page.tsx` with the feature body and add `api.ts`
and `hooks.ts` per the `add-page` skill; keep the head via `PageHead`.
