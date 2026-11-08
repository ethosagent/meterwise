#!/usr/bin/env bash
# new-migration.sh <feature> — create internal/store/migrations/NNNN_<feature>.sql
# from the template, refusing to touch an existing file, then regenerate docs/schema.md
# (and sqlc when configured). Prints the new path.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
. scripts/lib.sh
feature="${1:?usage: new-migration.sh <feature>   (lowercase, e.g. workspaces)}"
[[ $feature =~ ^[a-z][a-z0-9_]*$ ]] || { echo "✗ feature must match ^[a-z][a-z0-9_]*$" >&2; exit 1; }
dir=internal/store/migrations
mkdir -p "$dir"
num="$(next_num "$dir" 4)"
path="$dir/${num}_${feature}.sql"
[ ! -e "$path" ] || { echo "✗ $path exists; an applied migration is never edited" >&2; exit 1; }
if ls "$dir"/[0-9][0-9][0-9][0-9]_"${feature}".sql >/dev/null 2>&1; then
  # the feature already has tables: a later migration alters them, it never re-creates them
  cat >"$path" <<SQL
-- feature: ${feature}
-- +goose Up
-- Alter this feature's existing tables, for example:
-- ALTER TABLE ${feature} ADD COLUMN note text;

-- +goose Down
-- ALTER TABLE ${feature} DROP COLUMN note;
SQL
else
  cat >"$path" <<SQL
-- feature: ${feature}
-- +goose Up
CREATE TABLE ${feature} (
  id uuid PRIMARY KEY,
  org_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE ${feature};
SQL
fi
go run ./cmd/gen schema >/dev/null
if [ -f sqlc.yaml ]; then gotool sqlc generate; fi
echo "$path"
