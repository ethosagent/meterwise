---
name: add-page
description: Add a web page as a vertical slice under web/src/features/<name>: route, page, query hook, hasPermission guard, README and a test. Use after the React shell exists.
---

# Add a page

Rules first:
- One folder per feature: `web/src/features/<name>/{Page.tsx,api.ts,hooks.ts,Page.test.tsx,README.md}`.
  No `components/` dumping ground, no `index.ts` barrels (gate `web`).
- Server state in TanStack Query, UI state in Zustand; design tokens only, no hex colours or pixel literals.
- Gate the route and the actions with `hasPermission`; never infer a role in a component.
- Types come from `web/src/contract/generated.ts`; never hand-write a DTO twin (gate `generated`).
- No `any` (biome `noExplicitAny`). New npm package = allowlist entry in archtest plus an ADR.

## Steps

1. `.agents/skills/add-page/scripts/new-page.sh <name> <permission>` creates the slice (needs the React shell).
2. Write the failing test, then fill `api.ts`, `hooks.ts`, `Page.tsx`; register the route.
3. README.md (at most 120 lines): what the page shows, which endpoints it calls, which permission it needs.
4. `scripts/run-checks.sh web generated size`.
