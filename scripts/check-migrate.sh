#!/usr/bin/env bash
# Gate: migrations are well-formed, append-only and reversible.
#   1. cmd/gen migrations: NNNN_<feature>.sql, `-- feature:` header, a Down section,
#      and one feature's tables per file.
#   2. An applied migration is never edited: against the merge-base with main only
#      new files (status A) may appear under internal/store/migrations.
#   3. goose up -> down to 0 -> up on a THROWAWAY database of the loopback Postgres
#      (TEST_DATABASE_URL, or the compose Postgres), dropped afterwards.
# No migrations yet = explicit SKIP (ruling R5). Once one exists the gate fails
# closed, including when no Postgres is reachable.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
dir=internal/store/migrations
if ! ls "$dir"/*.sql >/dev/null 2>&1; then
  skip migrate "no migrations yet in $dir (Plan 01 adds the first); this gate fails closed once one exists"
  exit 0
fi

go run ./cmd/gen migrations

if base="$(git merge-base HEAD main 2>/dev/null)" && [ -n "$base" ]; then
  changed="$(git diff --name-status "$base" -- "$dir" | grep -v -E '^A[[:space:]]' || true)"
  if [ -n "$changed" ]; then
    echo "✗ applied migrations were modified or removed (add a NEW migration instead):"
    echo "$changed"
    exit 1
  fi
else
  skip migrate-immutability "no 'main' branch to compare against"
fi

command -v psql >/dev/null 2>&1 || { echo "✗ psql not found; install the PostgreSQL client"; exit 1; }
admin="$(ensure_pg)" || { echo "✗ no Postgres reachable: set TEST_DATABASE_URL or start docker (make db-up)"; exit 1; }
name="mw_migrate_$$_$RANDOM"
psql "$admin" -v ON_ERROR_STOP=1 -qc "CREATE DATABASE $name" >/dev/null ||
  { echo "✗ cannot create a throwaway database on the admin Postgres (is it reachable and is the role allowed CREATEDB?)"; exit 1; }
trap 'psql "$admin" -qc "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null 2>&1 || true' EXIT
dsn="$(printf '%s' "$admin" | sed -E "s#(/)[^/?]+(\\?|\$)#\\1$name\\2#")"
goose() { gotool goose -dir "$dir" postgres "$dsn" "$@"; }
tables() { psql "$dsn" -qtAc "select count(*) from information_schema.tables where table_schema = 'public' and table_name <> 'goose_db_version'"; }
goose up
[ "$(tables)" -gt 0 ] || { echo "✗ goose up created no tables"; exit 1; }
goose down-to 0
left="$(tables)"
[ "$left" -eq 0 ] || { echo "✗ down-to 0 left $left table(s) behind: a Down section does not undo its Up"; exit 1; }
goose up
if goose status | grep -q Pending; then echo "✗ migrations still pending after the final up"; exit 1; fi
echo "✓ goose up, down-to 0 (schema empty), up on throwaway database $name"
