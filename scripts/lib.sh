# Sourced by scripts/check-*.sh, .claude/hooks/*.sh and skill scripts.
# Shared, deterministic helpers. Caps here are the single source for
# docs/context-budget.md (Context Budget Playbook).
# shellcheck shell=bash

FAST_GATES=(fmt vet arch docs size sqlc) # seconds-cheap gates: Stop hook, handoff, --fast
MAX_SKILLS=40                       # project skills visible per session (exclusive)
MAX_SKILL_DESC=250                  # SKILL.md description, characters
MANUAL_SKILLS=(demo-traffic handoff) # must carry disable-model-invocation: true
COVERAGE_MIN_DEFAULT=75             # Go total statement coverage, percent

# gotool runs a tool pinned in tools/go.mod (ADR 0005). CGO_ENABLED=0: every
# pinned tool is pure Go, so a broken host C toolchain cannot break the gate.
# Do not use it for `go test -race`, which needs cgo. `-n` prints the cached
# binary, which then runs with the caller's env; GOROOT/bin goes first on PATH
# as `go tool` itself does.
gotool() {
  local bin goroot
  bin="$(GOWORK=off CGO_ENABLED=0 go tool -modfile="$REPO_ROOT/tools/go.mod" -n "$1")" || return 1
  goroot="$(go env GOROOT)" || return 1
  shift
  CGO_ENABLED=0 PATH="$goroot/bin:$PATH" "$bin" "$@"
}

# REPO_ROOT is absolute so helpers also work from inside a subdirectory.
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# skip GATE REASON prints the explicit, always-visible SKIP line (ruling R5):
# a gate whose inputs do not exist yet says so, never silently passes. run-checks.sh
# re-prints every line starting with "! " on a green gate.
skip() { echo "! SKIP $1: $2"; }

# cover_gate runs the Go tests offline with the race detector and a coverage
# profile, and fails below COVERAGE_MIN (percent). Skipped tests are surfaced as
# "! SKIP" lines so a green run never hides a test that did not run.
cover_gate() {
  local prof="$REPO_ROOT/.logs/coverage.out" vlog="$REPO_ROOT/.logs/test-verbose.log" total rc=0
  GOFLAGS=-mod=readonly go test -tags=offline -race -count=1 -v -coverprofile="$prof" -covermode=atomic ./... >"$vlog" 2>&1 || rc=$?
  # failures only; the full verbose log stays on disk
  { grep -E -- '^(--- FAIL|FAIL|panic:)|^\s+[a-z_]+\.go:[0-9]+' "$vlog" || true; } | head -n 80
  # a skipped test is printed as a visible "! SKIP" line, never swallowed
  awk '/^[[:space:]]*--- SKIP/ { name=$3; getline; sub(/^[[:space:]]+/, ""); print "! SKIP test " name ": " $0 }' "$vlog"
  [ "$rc" -eq 0 ] || return 1
  total="$(go tool cover -func="$prof" | awk '/^total:/ {gsub("%","",$3); print $3}')" || return 1
  echo "coverage: ${total}% (min ${COVERAGE_MIN}%)"
  if [ -z "$total" ] || awk -v t="$total" -v m="$COVERAGE_MIN" 'BEGIN { exit !(t < m) }'; then
    echo "✗ coverage ${total:-unknown}% is below ${COVERAGE_MIN}%. Add tests; see $prof (go tool cover -func=$prof)."
    return 1
  fi
}

# ensure_pg prints a loopback DSN for the compose Postgres, starting it when
# docker is available. It prints nothing and returns 1 when no database can be
# reached, or when TEST_DATABASE_URL names a non-loopback host or has no database
# name: gates run CREATE/DROP DATABASE, so they only ever touch a local server
# (ruling R6). METERWISE_PG_PORT overrides the host port (default 54329).
PG_PORT="${METERWISE_PG_PORT:-54329}"
PG_DSN_DEFAULT="postgres://meterwise:meterwise@127.0.0.1:${PG_PORT}/meterwise?sslmode=disable"

# dsn_problem DSN prints why a DSN is unusable for gates (empty when fine).
dsn_problem() {
  local dsn="$1" auth host
  case "$dsn" in postgres://* | postgresql://*) ;; *) echo "not a postgres:// URL"; return ;; esac
  auth="$(printf '%s' "$dsn" | sed -E 's#^[a-z]+://([^/?]*).*#\1#')"
  case "$auth" in *,*) echo "multiple hosts are not allowed"; return ;; esac
  if printf '%s' "$dsn" | grep -qiE '[?&](host|hostaddr|service)='; then echo "host/hostaddr/service query parameters are not allowed"; return; fi
  host="$(printf '%s' "${auth##*@}" | sed -E 's#^(\[[^]]*\]|[^:]*).*#\1#')"
  if ! printf '%s' "$host" | grep -qE '^(localhost|127(\.[0-9]+){3}|\[::1\])$'; then echo "host '$host' is not loopback"; return; fi
  printf '%s' "$dsn" | grep -qE '^[a-z]+://[^/?]*/[^/?]+' || echo "no database name in the URL path"
}

ensure_pg() {
  if [ -n "${TEST_DATABASE_URL:-}" ]; then
    local why
    why="$(dsn_problem "$TEST_DATABASE_URL")"
    if [ -n "$why" ]; then echo "✗ TEST_DATABASE_URL rejected: $why (gates only touch a local Postgres)" >&2; return 1; fi
    printf '%s\n' "$TEST_DATABASE_URL"
    return 0
  fi
  if ! pg_isready -q -h 127.0.0.1 -p "$PG_PORT" 2>/dev/null; then
    command -v docker >/dev/null 2>&1 || return 1
    (cd "$REPO_ROOT" && METERWISE_PG_PORT="$PG_PORT" docker compose up -d --wait postgres >/dev/null 2>&1) || return 1
  fi
  # whatever listens on the port must accept our credentials, or it is not ours
  if command -v psql >/dev/null 2>&1; then
    psql "$PG_DSN_DEFAULT" -qtAc 'select 1' >/dev/null 2>&1 || return 1
  fi
  printf '%s\n' "$PG_DSN_DEFAULT"
}

# sqlc_state prints "none", "ready" or "error: <why>". SKIP is only legal when no
# sqlc input of any shape exists (ruling R5): a misnamed config (sqlc.yml, sqlc.json,
# a nested one), queries or generated output without sqlc.yaml are errors, never SKIPs.
sqlc_state() {
  local cfgs q o
  cfgs="$(list_files | grep -E '(^|/)sqlc\.(ya?ml|json)$' || true)"
  q="$(list_files internal/store/queries | head -n 1)"
  o="$(list_files internal/store/sqlc | head -n 1)"
  if [ -n "$cfgs" ] && [ "$cfgs" != "sqlc.yaml" ]; then
    echo "error: sqlc config must be exactly one root sqlc.yaml (found: $(echo "$cfgs" | tr '\n' ' '))"; return
  fi
  if [ -z "$cfgs" ]; then
    if [ -n "$q$o" ]; then echo "error: sqlc inputs exist (internal/store/queries or internal/store/sqlc) but there is no sqlc.yaml"; return; fi
    echo none; return
  fi
  if [ -z "$q" ]; then echo "error: sqlc.yaml exists but internal/store/queries/ has no files"; return; fi
  echo ready
}

# list_files PATTERN... lists tracked and untracked-but-not-ignored files, so
# .gitignore (plan/, .logs/, reports/, ...) is the only exclusion list.
list_files() { git ls-files --cached --others --exclude-standard -- "$@" | sort -u; }

# size_limit PATH sets HARD and SOFT line caps for a repo-relative path
# (HARD empty = uncapped). First match wins.
size_limit() {
  HARD="" SOFT=""
  case "$1" in
    CLAUDE.md | AGENTS.md)                    HARD=150 ;;
    ARCHITECTURE.md)                          HARD=300 ;;
    docs/decisions/README.md)                 HARD=150 ;;
    docs/decisions/[0-9][0-9][0-9][0-9]-*.md) HARD=120 ;;
    .agents/skills/*/SKILL.md)                HARD=300 ;;
    .agents/skills/*/references/*.md)         HARD=200 ;;
    .claude/rules/*.md)                       HARD=60 ;;
    docs/runbooks/*.md)                       HARD=200 ;;
    tools/*)                                  ;; # separate module, not ours to cap
    web/src/contract/generated.ts | docs/schema.md | web/package-lock.json) ;; # generated, uncapped
    web/src/features/*/README.md)             HARD=120 ;;
    *.test.ts | *.test.tsx)                   HARD=500 ;;
    *.ts | *.tsx)                             HARD=400 SOFT=250 ;;
    *_test.go)                                HARD=500 ;;
    *.go)                                     HARD=800 SOFT=300 ;;
    *.md)                                     HARD=200 ;;
  esac
  SOFT="${SOFT:-$HARD}"
}

# size_verdict PATH LINES prints a finding; returns 1 over the hard cap. A soft
# cap only warns ("! ..."), and run-checks.sh keeps warnings visible.
size_verdict() {
  size_limit "$1"
  [ -n "$HARD" ] || return 0
  if [ "$2" -gt "$HARD" ]; then
    echo "✗ $1 has $2 lines (cap $HARD). Split it by idea; see docs/context-budget.md."
    return 1
  fi
  [ "$2" -le "$SOFT" ] || echo "! $1 has $2 lines (soft cap $SOFT). Consider splitting before it grows."
}

# check_size PATH: size_verdict for one existing file (used per edit by the hook).
check_size() { [ -f "$1" ] || return 0; size_verdict "$1" "$(( $(wc -l <"$1") ))"; }

# slugify "Some Title" -> some-title
slugify() { printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-|-$//g'; }

# next_num DIR WIDTH -> next zero-padded number after the highest NNN- prefix in DIR.
next_num() {
  local last
  last="$(ls "$1" 2>/dev/null | grep -Eo "^[0-9]{$2}" | sort -n | tail -1)"
  printf "%0${2}d" $((10#${last:-0} + 1))
}

# active_plan -> newest plan/NN[a]-*.md, or nothing (plan/ is gitignored and may not exist: ruling R1).
active_plan() { ls -t plan/[0-9][0-9]*-*.md 2>/dev/null | head -n 1 || true; }
