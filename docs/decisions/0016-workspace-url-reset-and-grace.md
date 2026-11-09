# 0016. The workspace URL is the credential: reset with grace, optional CIDR guard, no proxy key

- **Status:** Accepted
- **Date:** 2026-11-10
- **Source:** plan/04-workspace-url-setup-and-audit-log.md, 04-interface.md; builds on [0003](0003-unguessable-workspace-url-is-the-credential.md)

## Context

[0003](0003-unguessable-workspace-url-is-the-credential.md) makes `https://host/w/{publicId}` the
only credential of the data plane. A credential needs rotation, a way to limit where it works from,
and a clear answer when a client uses a rotated one. Meterwise never holds a provider key
(non-negotiable 2), so a second "Meterwise API key" would only add a secret to manage.

## Decision

1. **No proxy key.** Clients keep their own provider key (`Authorization` or `x-api-key`), which
   passes through untouched. The authenticator reads neither header and adds none.
2. **Reset** (`POST /api/workspaces/{id}/url/reset`, `workspace_url:manage`) draws a new 22-char
   base62 id from `crypto/rand` (about 131 bits), retries on a collision, and in one transaction
   retires the current id with `grace_until = now + graceMinutes` (0 to 1440, default 10) and
   makes the new id current. A partial unique index allows one current id per workspace.
   `retire-previous` ends every open grace at once.
3. **Past grace the old URL is 410, not 404.** The directory returns `workspace_url_reset` for a
   retired id past its grace; an id that never existed stays `not_found` (404). The distinction is
   the signal a client needs ("this URL was rotated"). tollgate fixes the body text by status
   alone (ADR 0008 of tollgate), so only the status carries it.
4. **Allowed client CIDRs** (`PUT .../url/allowed-cidrs`, at most 32, bare IP becomes /32 or /128,
   `[]` clears). The authenticator compares the TCP peer address; `X-Forwarded-For` is not read,
   because a client can write it. Behind a load balancer the peer is the balancer, so a deployment
   that terminates TLS in front must leave the list empty until a trusted-proxy setting exists.
5. **The URL is gated in the API**: `publicId`, `proxyUrl` and `allowedClientCidrs` are null
   unless the caller holds `workspace_url:read` on that workspace, in every response that carries a
   workspace. Neither a public id nor a URL goes into a log line or audit metadata.

## Consequences

Easier: leaking a URL is recoverable (reset, then retire-previous), and a client that missed the
rotation gets a precise 410. Harder: a retired id stays valid for the grace window by design, and
a cache holds it no longer than `grace_until`. Known gap: the CIDR guard is per peer address only.
Enforced by: `TestResetKeepsTheOldIDAnsweringUntilItsGraceEnds` and the reset tests in
`internal/store`, `internal/tenancy` and `internal/api`, `internal/wsauth` tests over a real
tollgate proxy, `TestTheURLIsNullWithoutWorkspaceURLRead`.
