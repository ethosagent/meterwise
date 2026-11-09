# 0013. The Clerk Go SDK and Svix verify identity and webhooks

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 02 sections 3 and 6; archtest H6

## Context

Plan 02 verifies Clerk session JWTs against a JWKS, creates and revokes Clerk invitations and accepts
Clerk's user webhooks, which Clerk signs with Svix. Options: write the JWT, JWKS and HMAC code ourselves
(a security-critical surface, easy to get subtly wrong: algorithm confusion, key-id handling, timing),
or import the vendors' libraries for the parts they own (chosen).

## Decision

`github.com/clerk/clerk-sdk-go/v2` (already allowlisted for `identity`) supplies JWT parsing and
verification (`jwt.Verify`, which pins the algorithm to the JWK's and rejects `alg=none`) and the
invitations client. `github.com/svix/svix-webhooks` supplies the webhook signature check
(`Webhook.VerifyIgnoringTimestamp`; we check the 5-minute window with an injected clock and add replay
protection by message id). Both are imported **only** by the `identity` layer (archtest H6 entries in
`internal/archtest/law_test.go`); no other package sees an SDK type. We add what the libraries do not
do: an exact `iss` match (the SDK's own check is a prefix test unless a proxy URL is given, so we pass the
configured issuer as the proxy URL and compare again), a rate-limited JWKS refetch for unknown key ids,
and a single opaque webhook error.

## Consequences

Two new module dependencies (the SDK pulls `go-jose`). They are third-party code on the sign-in path:
`govulncheck` covers them in the `vuln` gate and upgrades go through the normal review lenses. Tests never
reach Clerk: `internal/identity/clerkmock` signs real RS256 tokens against a local JWKS and answers the
backend API, so the adapter is exercised end to end offline. Live Clerk is **UNVERIFIED** (ruling R7).
Enforced by: archtest H6 (layer allowlist, with a seeded violation test), `TestVerifyRejectsEveryForgery`,
`TestWebhookRejections`.
