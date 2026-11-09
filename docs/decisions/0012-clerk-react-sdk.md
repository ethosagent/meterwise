# 0012. Clerk React SDK for sign-in, behind an auth adapter

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 02 section 2 and 7; decision D3; ADR 0007

## Context

Plan 02 puts Clerk in front of the app: themed login, forgot, reset and accept-invite screens, with sign-ups disabled. Options: build the screens against Clerk's JS API by hand (more code, we own password-reset and ticket flows), or use the Clerk React components (chosen). `@clerk/clerk-react` is deprecated by Clerk in favour of `@clerk/react`, so the new package is the one to allowlist.

## Decision

- `@clerk/react` is pinned exactly in `web/package.json` (6.18.6) and added to `webAllow` in `internal/archtest/scan_test.go` (rule H5).
- Only `web/src/auth/clerk.tsx` imports it. Everything else talks to the `AuthAdapter` interface (`web/src/auth/adapter.ts`). A mock adapter (`auth/mock.tsx`) serves tests and the dev server and is behind `import.meta.env.DEV`, so it is not in a production bundle.
- The bearer token is attached in one place, `api/client.ts`. A production bundle without `VITE_CLERK_PUBLISHABLE_KEY` shows a "Sign-in is not configured" screen and never falls back to the mock. `npm run build:release` (used by the Dockerfile and `make web`) fails outright without the key; plain `npm run build` stays keyless so the CI gate does not need an instance.
- Clerk components are themed from antd tokens (`appearance.variables`), so light and dark follow the app.

## Consequences

The SDK adds about 100 kB (25 kB gzip) in its own lazily loaded chunk. Clerk's hosted pieces (SignIn and SignUp UI) load from Clerk's CDN at runtime. The Clerk screens were never exercised against a live instance (needs live credentials); only the adapter seam and the mock are tested. Swapping provider means writing one adapter.
