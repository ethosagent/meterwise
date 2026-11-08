# Tenancy and authorization (`TNT-*`)

Guards workspace isolation (decision D5) and the route-to-permission table.
Read `internal/contract/routes.go` first.

## Already covered by CI

- archtest H2 (layering) and H4 (one writer per ledger table).
- `generated` gate: openapi.json and the web route twin come from the route table.

## Checks

**TNT-1 Every query is scoped.** Each sqlc query on a workspace-owned table
filters by `workspace_id`; each org-owned table query filters by `org_id`. A
query that takes only a row id is a finding unless the table is global.

**TNT-2 Every control-plane route has a row.** A handler registered without an
entry in the route-to-permission table (`internal/contract/routes.go`) is a
finding; a non-public row with an empty permission is a finding.

**TNT-3 URL ids are checked against the principal.** A `{workspace_id}` path
parameter is resolved through `WorkspaceDirectory` for the calling principal;
the handler never trusts the id because it parses.

**TNT-4 The public id resolves only through the directory.** The 22-char
`/w/{id}` value maps to a workspace in `wsauth` via `WorkspaceDirectory.ByPublicID`
and nowhere else; no other package reads that column.

**TNT-5 No inline role checks.** Handlers never compare `Role` strings; they rely
on the gateway middleware and the evaluator, so a policy change is one edit.

**TNT-6 Not-found beats forbidden across tenants.** A resource in another
workspace answers `not_found`, never `forbidden`, so ids cannot be probed.

**TNT-7 Retired ids answer `gone`.** A reset workspace URL returns `gone` inside
its grace window and `not_found` after it; neither reveals the new id.

**TNT-8 org_id stays on every table.** A new table has `org_id` (and
`workspace_id` where it belongs to one) even while there is a single org.
