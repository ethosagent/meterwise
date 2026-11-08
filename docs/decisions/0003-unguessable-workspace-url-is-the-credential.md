# 0003. The unguessable workspace URL is the only credential

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 00 D6, D7

## Context

Agents keep their own provider keys; a second proxy key would add setup for no isolation gain. Anyone with the URL can route traffic but pays their own provider bill. Options: proxy key header, signed URL, random id in the path (chosen).

## Decision

The proxy URL is `/w/{id}/{provider}/...` with a 22-char base62 id (about 130 bits), distinct from the display name. Reset URL issues a new id; the old one answers 410 for a grace window.

## Consequences

One-line setup. The risk is dashboard pollution only, covered by unguessability and reset. The id is a secret-lite value: never logged (`PRIV-6`), resolved only via `WorkspaceDirectory` (`TNT-4`). No `proxykeys` package exists.
