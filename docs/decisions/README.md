# Architecture decision records

Read the ADR for an area before you change it. Write a new one when a change
moves a boundary (skill: [`write-adr`](../../.agents/skills/write-adr/SKILL.md)).
Never edit an accepted ADR's decision; supersede it with a new one.

| ADR | Title | Status |
|---|---|---|
| [0001](0001-one-binary-for-proxy-and-control-plane.md) | One binary serves the proxy and the control plane | Accepted |
| [0002](0002-tollgate-tenant-is-the-workspace.md) | tollgate Tenant is the workspace | Accepted |
| [0003](0003-unguessable-workspace-url-is-the-credential.md) | The unguessable workspace URL is the only credential | Accepted |
| [0004](0004-ledger-lives-in-hook-after.md) | The ledger is written from Hook.After | Accepted |
| [0005](0005-postgres-only-with-pgx-sqlc-goose.md) | Postgres only, with pgx, sqlc and goose | Accepted |
| [0006](0006-two-savings-numbers-never-summed.md) | Saved and likely saved are two numbers, never summed | Accepted |
| [0007](0007-dependency-allowlists.md) | Dependency allowlists for Go layers and npm | Accepted |

Template: [`template.md`](template.md).

## Context baseline

`/context` on a fresh session in this repo (target at most 30K tokens; re-measure monthly:
[`runbooks/agent-hygiene.md`](../runbooks/agent-hygiene.md)):

| Date | Tokens | How measured | Notes |
|---|---|---|---|
| 2026-11-09 | UNVERIFIED | not measured yet | the owner runs `/context` in a fresh interactive session and records the number here |
