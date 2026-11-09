# 0011. Declare @testing-library/dom as a dev dependency

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 01 web review M-2; ADR 0007

## Context

`@testing-library/react` and `@testing-library/user-event` both need `@testing-library/dom` as a peer. It was installed only implicitly, so a lockfile regeneration under a stricter peer policy would break the test install. Options: leave it implicit (fragile), or declare it (chosen). It is the same project and release train as the two packages already on the H5 allowlist.

## Decision

`@testing-library/dom` is pinned exactly in `web/package.json` devDependencies (10.4.2, the version the lockfile already resolved) and added to `webAllow` in `internal/archtest/scan_test.go`.

## Consequences

The peer is explicit and locked. No source imports it directly; it exists for the two Testing Library packages. Enforced by archtest H5 (the scan of `web/package.json` in `internal/archtest/scan_test.go`, with its seeded violation in `seeded_test.go`) and the `web` gate's `npm ci`.
