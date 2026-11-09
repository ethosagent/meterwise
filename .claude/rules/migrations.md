---
paths:
  - "internal/store/migrations/**"
  - "internal/store/queries/**"
---

# Migration rules

This is a gates-tier path. Checklist: `.agents/reviews/data-integrity.md`. Use the `add-migration` skill.

- **New file only.** Never edit or delete an applied migration; the `migrate` gate compares with `main`.
- Name `NNNN_<feature>.sql`, first line `-- feature: <feature>`. One feature's tables per file.
- Feature names are specific (`workspaces`, not `misc`/`core`). A table's name must be the feature, its stem or `<stem>_...`; any other
  table needs a line in `internal/store/migration-families.txt` (protected). Only flat `NNNN_<feature>.sql` files live here.
- A real `-- +goose Down` that undoes Up. The gate runs up-by-one, down, up per migration and compares schemas.
- No data backfill in a schema migration (DATA-8). No `float` money: `numeric(14,6)` named `*_usd`.
- Every table keeps `org_id`; workspace-owned tables also `workspace_id`; timestamps are `timestamptz`.
- Queries: `queries/<feature>.sql`, one file per feature, every query scoped by `workspace_id` or `org_id` (TNT-1).
- Only `queries/ledger.sql` and `queries/audit.sql` may write `calls`, `sessions`, `monitor_events`, `audit_log` (H4).
- After a change run `scripts/run-checks.sh migrate sqlc generated` and commit the generated files.
