# 0002. tollgate Tenant is the workspace

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 00 D5

## Context

tollgate partitions sessions, admission and metrics by `Tenant`. The customer sees data per workspace, and the org is single for now. Options: Tenant = org, Tenant = workspace (chosen).

## Decision

`Tenant.ID` is the workspace id; `Attrs` carry `org_id` and the public id; `Policy` comes from workspace settings.

## Consequences

Isolation lines up with what the customer sees. Every table keeps `org_id` so multi-org stays additive. Enforced by: `TNT-*` lens, `wsauth` tests, queries scoped by `workspace_id`/`org_id`.
