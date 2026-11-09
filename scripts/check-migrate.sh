#!/usr/bin/env bash
# Gate: migrations are well-formed, append-only and reversible.
#   1. SQL anywhere except internal/store/migrations (flat NNNN_<feature>.sql) and
#      internal/store/queries is refused, so a misplaced migration is never a SKIP.
#   2. cmd/gen migrations: file name, `-- feature:` header, non-generic feature, a Down
#      section, and every table touched or created belongs to the file's one feature.
#   3. An applied migration is never edited: against the merge-base with main (or
#      origin/main when there is no local main; METERWISE_BASE_REF overrides, ruling R18) only new files (status A) may appear.
#      On main itself the last commit is compared. No base to compare with fails under
#      CI=true and prints a SKIP locally.
#      New migration numbers must exceed the newest one on the base branch.
#      An emptied migrations directory fails when the base branch tracks migrations.
#   4. goose, on a THROWAWAY database of a loopback Postgres (TEST_DATABASE_URL or the
#      compose Postgres): for every migration, up-by-one, down, up must reproduce the
#      schema exactly (pg_dump --schema-only, so a Down that leaves a sequence, an enum value
#      or a grant behind fails), then down-to 0 leaves no tables and up applies everything.
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

if [ -n "${METERWISE_BASE_REF:-}" ] && [ -z "$(base_ref)" ]; then
  echo "✗ METERWISE_BASE_REF=$METERWISE_BASE_REF does not resolve to a commit"
  exit 1
fi
if compare_ref; then
  mb="$CMP_MB"
  # net change since the merge-base, including the working tree
  diffs="$(git diff --name-status "$mb" -- "$dir")"
  # on the base branch itself HEAD == merge-base: also compare the last commit
  if [ "$CMP_LAST" -eq 1 ]; then
    diffs="$diffs
$(git diff --name-status HEAD~1 HEAD -- "$dir")"
  fi
  changed="$(printf '%s\n' "$diffs" | grep -v -E '^(A[[:space:]]|$)' || true)"
  # untracked new files are additions too (git diff does not list them)
  diffs="$diffs
$(git ls-files --others --exclude-standard -- "$dir" | sed 's/^/A\t/')"
  # numbers must increase: a new migration numbered below the newest applied one is out of order
  base_max="$(git ls-tree --name-only "$CMP_ORD" -- "$dir/" | sed 's#.*/##' | grep -oE '^[0-9]{4}' | sort | tail -n 1 || true)"
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
  echo "✗ no base branch (METERWISE_BASE_REF, main or origin/main) to compare with (shallow clone?): cannot prove applied migrations are unchanged. Use fetch-depth: 0."
  exit 1
else
  skip migrate-immutability "no base branch (METERWISE_BASE_REF, 'main' or 'origin/main') to compare against"
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

# pg_dump must not be older than the server. Against the compose Postgres it runs INSIDE the
# container (always version-matched); against another loopback server (TEST_DATABASE_URL) the
# local client is used and must be at least the server's major version.
[ -z "$(dsn_problem "$dsn")" ] || { echo "✗ throwaway DSN rejected: $(dsn_problem "$dsn")"; exit 1; }
in_container=0
if [ "$admin" = "$PG_DSN_DEFAULT" ] && [ -n "$(cd "$REPO_ROOT" && docker compose ps -q postgres 2>/dev/null)" ]; then in_container=1; fi
dump() {
  if [ "$in_container" -eq 1 ]; then
    (cd "$REPO_ROOT" && docker compose exec -T postgres pg_dump -U meterwise -d "$name" --schema-only --no-owner --exclude-table='goose_db_version*')
  else
    pg_dump "$dsn" --schema-only --no-owner --exclude-table='goose_db_version*'
  fi
}
if [ "$in_container" -eq 0 ]; then
  command -v pg_dump >/dev/null 2>&1 || { echo "✗ pg_dump not found; install the PostgreSQL client (or use the compose Postgres: make db-up)"; exit 1; }
  server_major="$(( $(psql "$dsn" -qtAX -c 'show server_version_num') / 10000 ))"
  client_major="$(pg_dump --version | grep -oE '[0-9]+' | head -n 1)"
  if [ "$client_major" -lt "$server_major" ]; then
    echo "✗ local pg_dump $client_major is older than the Postgres $server_major server: install a matching client or use the compose Postgres (make db-up)"
    exit 1
  fi
fi

# A deterministic description of the whole schema (pg_dump --schema-only: tables, columns,
# indexes, constraints, triggers, policies, views, functions, types and enum values, sequences,
# grants), excluding goose's own table. Volatile lines are dropped: \restrict/\unrestrict
# carry a random token, and the "Dumped from/by" comments carry client and server versions.
snap() {
  local out
  out="$(dump)" || { echo "✗ pg_dump failed" >&2; return 1; }
  printf '%s\n' "$out" | grep -vE '^\\(un)?restrict |^-- Dumped (from|by) '
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
