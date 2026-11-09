#!/usr/bin/env bash
# Gate: migrations are well-formed, append-only and reversible.
#   1. SQL anywhere except internal/store/migrations (flat NNNN_<feature>.sql) and
#      internal/store/queries is refused, so a misplaced migration is never a SKIP.
#   2. cmd/gen migrations: file name, `-- feature:` header, non-generic feature, a Down
#      section, and every table touched or created belongs to the file's one feature.
#   3. An applied migration is never edited: against the merge-base with main (or
#      origin/main when there is no local main) only new files (status A) may appear.
#      On main itself the last commit is compared. No base to compare with fails under
#      CI=true and prints a SKIP locally.
#      New migration numbers must exceed the newest one on the base branch.
#      An emptied migrations directory fails when the base branch tracks migrations.
#   4. goose, on a THROWAWAY database of a loopback Postgres (TEST_DATABASE_URL or the
#      compose Postgres): for every migration, up-by-one, down, up must reproduce the
#      schema exactly (a Down that does not undo its Up fails), then down-to 0 leaves
#      no tables and up applies everything.
# SKIP only when no SQL input of any shape exists (ruling R5); with inputs it fails closed,
# including when no Postgres is reachable.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
dir=internal/store/migrations

stray="$(list_files | grep -iE '\.sql$' | grep -v -E '^internal/store/queries/|(^|/)testdata/' | grep -v -E "^$dir/[^/]+\.sql\$" || true)"
if [ -n "$stray" ]; then
  echo "✗ SQL outside $dir (flat, lower-case .sql) and internal/store/queries:"
  echo "$stray" | sed 's/^/    /'
  exit 1
fi
if [ -z "$(ls -A "$dir" 2>/dev/null)" ]; then
  if tracked_on_base "$dir"; then
    echo "✗ $dir is empty but the base branch tracks migrations there: applied migrations were deleted (add a NEW migration instead)"
    exit 1
  fi
  skip migrate "no migrations exist yet in $dir (Plan 01 adds the first); this gate fails closed the moment one does"
  exit 0
fi

go run ./cmd/gen migrations

base_ref=""
for r in main origin/main; do
  if git rev-parse --verify -q "$r^{commit}" >/dev/null; then base_ref="$r"; break; fi
done
if [ -n "$base_ref" ] && mb="$(git merge-base HEAD "$base_ref" 2>/dev/null)" && [ -n "$mb" ]; then
  # net change since the merge-base, including the working tree
  diffs="$(git diff --name-status "$mb" -- "$dir")"
  # on the base branch itself HEAD == merge-base: also compare the last commit
  if [ "$mb" = "$(git rev-parse HEAD)" ] && git rev-parse --verify -q HEAD~1 >/dev/null; then
    diffs="$diffs
$(git diff --name-status HEAD~1 HEAD -- "$dir")"
  fi
  changed="$(printf '%s\n' "$diffs" | grep -v -E '^(A[[:space:]]|$)' || true)"
  # numbers must increase: a new migration numbered below the newest applied one is out of order
  ord_ref="$mb"
  if [ "$mb" = "$(git rev-parse HEAD)" ] && git rev-parse --verify -q HEAD~1 >/dev/null; then ord_ref="HEAD~1"; fi
  base_max="$(git ls-tree --name-only "$ord_ref" -- "$dir/" | sed 's#.*/##' | grep -oE '^[0-9]{4}' | sort | tail -n 1 || true)"
  if [ -n "$base_max" ]; then
    while IFS=$'\t' read -r st f; do
      [ "$st" = A ] || continue
      n="$(basename "$f" | grep -oE '^[0-9]{4}' || true)"
      if [ -n "$n" ] && [ ! "$n" \> "$base_max" ]; then
        echo "✗ $f is numbered $n but $base_max is already applied on the base branch: new migrations must use a higher number (renumber it)"
        exit 1
      fi
    done <<<"$diffs"
  fi
  if [ -n "$changed" ]; then
    echo "✗ applied migrations were modified or removed (add a NEW migration instead):"
    echo "$changed"
    exit 1
  fi
elif [ "${CI:-}" = "true" ]; then
  echo "✗ no main or origin/main to compare with (shallow clone?): cannot prove applied migrations are unchanged. Use fetch-depth: 0."
  exit 1
else
  skip migrate-immutability "no 'main' or 'origin/main' to compare against"
fi

command -v psql >/dev/null 2>&1 || { echo "✗ psql not found; install the PostgreSQL client"; exit 1; }
admin="$(ensure_pg)" || { echo "✗ no usable Postgres: set a loopback TEST_DATABASE_URL (with a database name) or start docker (make db-up)"; exit 1; }
name="mw_migrate_$$_$RANDOM"
psql "$admin" -v ON_ERROR_STOP=1 -qc "CREATE DATABASE $name" >/dev/null ||
  { echo "✗ cannot create a throwaway database on the admin Postgres (is it reachable and is the role allowed CREATEDB?)"; exit 1; }
trap 'psql "$admin" -qc "DROP DATABASE IF EXISTS $name WITH (FORCE)" >/dev/null 2>&1 || true' EXIT
dsn="$(printf '%s' "$admin" | sed -E "s#^([a-z]+://[^/?]*/)[^/?]+#\\1$name#")"
case "$dsn" in *"/$name"*) ;; *) echo "✗ could not point the DSN at the throwaway database"; exit 1 ;; esac
goose() { gotool goose -dir "$dir" postgres "$dsn" "$@"; }

# A deterministic description of the public schema, excluding goose's own table.
snap() {
  psql "$dsn" -qtAX -v ON_ERROR_STOP=1 <<'SQL'
select x from (
  select 'col ' || table_name || '.' || column_name || ' ' || data_type || ' ' || udt_name || ' len=' || coalesce(character_maximum_length::text, '') || ' prec=' || coalesce(numeric_precision::text, '') || ',' || coalesce(numeric_scale::text, '') || ' ' || is_nullable || ' ' || coalesce(column_default, '') as x
    from information_schema.columns where table_schema = 'public' and table_name <> 'goose_db_version'
  union all select 'idx ' || indexdef from pg_indexes where schemaname = 'public' and tablename <> 'goose_db_version'
  union all select 'con ' || conrelid::regclass || ' ' || conname || ' ' || pg_get_constraintdef(oid)
    from pg_constraint where connamespace = 'public'::regnamespace and conrelid::regclass::text <> 'goose_db_version'
  union all select 'trg ' || pg_get_triggerdef(oid) from pg_trigger where not tgisinternal
  union all select 'pol ' || tablename || ' ' || policyname || ' ' || coalesce(qual, '') || coalesce(with_check, '') from pg_policies where schemaname = 'public'
  union all select 'view ' || viewname || ' ' || definition from pg_views where schemaname = 'public'
  union all select 'func ' || proname from pg_proc where pronamespace = 'public'::regnamespace
  union all select 'type ' || typname from pg_type where typnamespace = 'public'::regnamespace and typtype in ('e', 'd')
) s order by x
SQL
}

count="$(ls "$dir"/*.sql | wc -l | tr -d ' ')"
prev="$(snap)"
for i in $(seq 1 "$count"); do
  goose up-by-one >/dev/null
  cur="$(snap)"
  goose down >/dev/null
  after="$(snap)"
  if [ "$after" != "$prev" ]; then
    echo "✗ migration #$i: Down does not restore the schema that existed before its Up. Difference (< before Up, > after Down):"
    diff <(echo "$prev") <(echo "$after") | head -n 20 || true
    exit 1
  fi
  goose up-by-one >/dev/null
  if [ "$(snap)" != "$cur" ]; then echo "✗ migration #$i: Up after Down produced a different schema"; exit 1; fi
  prev="$cur"
done
goose down-to 0 >/dev/null
left="$(psql "$dsn" -qtAX -c "select count(*) from information_schema.tables where table_schema = 'public' and table_name <> 'goose_db_version'")"
[ "$left" -eq 0 ] || { echo "✗ down-to 0 left $left table(s) behind"; exit 1; }
goose up >/dev/null
if goose status | grep -q Pending; then echo "✗ migrations still pending after the final up"; exit 1; fi
echo "✓ $count migration(s): each up/down/up reproduces the schema; down-to 0 leaves it empty; up applies all (throwaway database $name)"
