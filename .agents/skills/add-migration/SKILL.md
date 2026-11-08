---
name: add-migration
description: Add a Postgres migration for one feature with goose, regenerate sqlc and docs/schema.md. Use for any schema change. Never edit an applied migration; always add a new file.
---

# Add a migration

Rules first:
- **New file only.** An applied migration is never edited (gate `migrate` compares with `main`).
- **One feature per file**: `NNNN_<feature>.sql`, header `-- feature: <feature>`. It creates or alters
  only its own feature's tables; reference other tables by foreign key only.
- **Reversible**: a real `-- +goose Down`. No data backfill in a schema migration (DATA-8).
- Every table keeps `org_id` (and `workspace_id` where it belongs to one); money is `numeric(14,6)` `*_usd`.
- Queries live in `internal/store/queries/<feature>.sql`, one file per feature.

## Steps

1. `.agents/skills/add-migration/scripts/new-migration.sh <feature>` creates the next numbered file
   from the template and refuses if the name exists.
2. Write Up and Down. Add the sqlc queries for the feature in `internal/store/queries/<feature>.sql`.
3. `scripts/run-checks.sh migrate sqlc generated`: lints, runs goose up, down-to 0, up on a throwaway
   database, regenerates sqlc and `docs/schema.md` (commit the generated files).
4. Review with the `data-integrity` lens (`.agents/reviews/data-integrity.md`).
