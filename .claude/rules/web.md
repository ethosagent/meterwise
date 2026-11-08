---
paths:
  - "web/src/**"
---

# Web rules

Stack: React, Vite, Ant Design, TanStack Query, Zustand, React Router (decision D2). Use the `add-page` skill.

- One folder per feature: `web/src/features/<name>/{Page.tsx,api.ts,hooks.ts,*.test.tsx,README.md}`; README at most 120 lines.
- No `index.ts` barrels, no `any`, strict `tsc`. Imports stay inside `web/src/**` or the npm allowlist (H5).
- Server state in TanStack Query; UI state in Zustand. Design tokens only: no hex colours, no pixel literals.
- Gate routes and actions with `hasPermission`; never infer a role in a component (TNT-5).
- Types come from `web/src/contract/generated.ts`; it is generated, never edited or read whole.
- Files over 250 lines warn and over 400 fail (tests 500); split by idea.
- Run checks through `scripts/run-checks.sh web` or `scripts/quiet.sh npm run test`.
