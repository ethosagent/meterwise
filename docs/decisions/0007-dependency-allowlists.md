# 0007. Dependency allowlists for Go layers and npm

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 00a section 3.2, H5 and H6

## Context

Each new dependency is a long-term cost and an attack surface. The stack is decided (D2, D3, D9); unplanned packages are not. Options: free adds, an allowlist with an ADR per addition (chosen).

## Decision

`internal/archtest/law_test.go` lists third-party Go prefixes per layer (tollgate, pgx and goose in `store`, chi in `api`, the Clerk SDK in `identity`, otel in `dataplane`/`api`/`cmd`) and the npm allowlist (the D2 stack and the pinned dev toolchain). Adding to either needs an ADR.

## Consequences

A new package is a deliberate, reviewed act. The lists are protected paths. Enforced by: archtest H5 and H6, each with a seeded violation test.
