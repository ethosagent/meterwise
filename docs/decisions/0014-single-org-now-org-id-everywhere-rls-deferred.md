# 0014. Single org now, org_id everywhere, RLS deferred

- **Status:** Accepted
- **Date:** 2026-11-09
- **Source:** Plan 02 sections 1 and 4; decisions D4, D9, D13

## Context

Meterwise starts as one organization, invite-only, with many workspaces (D4). A multi-tenant product
needs signup, an org switcher and row-level security (RLS). Building those now is cost without a user;
retrofitting `org_id` onto every table later is a data migration. Options: no `org_id` (cheap, painful
later), `org_id` plus RLS now (complete, large), `org_id` everywhere with one seeded org and no RLS (chosen).

## Decision

Every table keeps an `org_id` that points at the one `organizations` row, which migration `0002` seeds with
the constant id `store.OrgID()` and a unique index on a constant expression keeps at exactly one row. Every
query is scoped by `org_id` (and `workspace_id` where the table has one), so a workspace id outside the org
answers 404 today by construction. There is no RLS, no org switcher and no self-serve signup: people join
through an admin invitation (`complete-signup` matches a verified email to a pending invitation). Operational
limits such as `MAX_WORKSPACES` are environment config, not a `plans` table (D13).

## Consequences

Going multi-org later is additive: drop the single-row index, add signup, an org switcher and RLS policies on
the existing `org_id` columns; no existing row moves. The cost today is discipline: a query without an org
scope is a bug that RLS would otherwise catch (lens TNT-1). Enforced by: the `workspace ids outside the org
are 404` test (lifts the single-row index on a throwaway database to build a second org), the
`only one organization` test, and the migrations lint (every table keeps `org_id`).
