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
| [0008](0008-web-design-tokens-derived-from-the-plan-and-prototype.md) | Web design tokens derived from the plan and prototype | Accepted |
| [0010](0010-control-plane-stack.md) | Control plane on the stdlib mux, with OTel API and OTLP/HTTP | Accepted |
| [0011](0011-declare-testing-library-dom.md) | Declare @testing-library/dom as a dev dependency | Accepted |
| [0012](0012-clerk-react-sdk.md) | Clerk React SDK for sign-in, behind an auth adapter | Accepted |
| [0013](0013-clerk-go-sdk-and-svix-for-identity.md) | The Clerk Go SDK and Svix verify identity and webhooks | Accepted |
| [0014](0014-single-org-now-org-id-everywhere-rls-deferred.md) | Single org now, org_id everywhere, RLS deferred | Accepted |
| [0015](0015-rbac-kernel-grants-one-gateway-last-admin-lock.md) | RBAC kernel: grants, one gateway, last-admin lock | Accepted |
| [0016](0016-workspace-url-reset-and-grace.md) | The workspace URL is the credential: reset with grace, optional CIDR guard, no proxy key | Accepted |
| [0017](0017-authenticator-inlinesafe-bounded-lookup.md) | The workspace authenticator is InlineSafe, with a bounded miss | Accepted |
| [0018](0018-audit-log-append-only-by-trigger.md) | The audit log is append-only by trigger and emitted after commit | Accepted |
| [0019](0019-ledger-write-path-queue-batcher-idempotent.md) | The ledger write path: Hook.After, a bounded queue, a batcher, idempotent rows | Accepted |
| [0020](0020-money-as-microusd-in-pricing-and-ledger.md) | Money is micro-USD in pricing and the ledger | Accepted |
| [0021](0021-per-workspace-admission-override-without-restart.md) | Per-workspace admission override without a restart | Accepted |
| [0022](0022-monitor-verdicts-cross-the-chain-through-a-meta-pointer.md) | Monitor verdicts cross the chain through a Meta pointer; config via a tenant attr | Accepted |
| [0023](0023-two-savings-numbers-never-summed.md) | Two savings numbers, never summed | Accepted |
| [0024](0024-history-ring-keyed-by-call-index-bounded-by-deadline-and-bytes.md) | The history ring is keyed by call index and bounded by deadline and bytes | Accepted |
| [0025](0025-savings-read-model-event-time-utc-buckets-no-materialised-view-until-the-bench-says-so.md) | Savings read model: event-time UTC buckets, no materialised view until the bench says so | Accepted |
| [0026](0026-one-body-edit-protocol-internal-wire-and-the-09-11-hook-order.md) | One body-edit protocol (`internal/wire`) and the 09-11 hook order | Accepted |
| [0027](0027-savings-causes-beyond-monitors-one-read-model.md) | Savings causes beyond monitors: one read model, trim and router in the headline, memory likely | Accepted |
| [0029](0029-router-changes-one-field-the-customers-key-monitors-are-the-safety-net.md) | The router changes one field, on the customer's key; monitors are the safety net | Accepted |
| [0030](0030-router-target-unavailable-and-deferred-model-masking.md) | Router: a refused target pauses the pair; model masking and the route header are deferred | Accepted |

Template: [`template.md`](template.md).

## Context baseline

`/context` on a fresh session in this repo (target at most 30K tokens; re-measure monthly:
[`runbooks/agent-hygiene.md`](../runbooks/agent-hygiene.md)):

| Date | Tokens | How measured | Notes |
|---|---|---|---|
| 2026-11-09 | 32.8k of 400k | `claude -p "/context"` run non-interactively in the 00a worktree (model claude-opus-5-5) | Over the 30K target. With connectors off (`--strict-mcp-config --setting-sources project`) the same run reads 17.6k. Repo-owned: `CLAUDE.md` 3.2k, project skills about 0.5k, reader agent 63. The rest is user-level: MCP tools 8.9k and MCP instructions 5k (claude.ai connectors), plugin and synced skills about 4k, system 3.4k, user `CLAUDE.md` 0.8k. Disable unused connectors for this repo ([`runbooks/local-dev.md`](../runbooks/local-dev.md)), re-measure interactively and record the result |
