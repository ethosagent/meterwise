#!/usr/bin/env bash
# Proves every gate goes RED on a bad input and stays GREEN on a good one.
# Each scenario copies the working tree to a scratch repo, applies one mutation and runs
# scripts/run-checks.sh for the gate under test, then checks the exit code AND that the
# output names the cause. "control" scenarios must pass (a gate that is always red proves nothing).
#
#   scripts/selftest-gates.sh              all scenarios
#   scripts/selftest-gates.sh arch web     only scenarios whose gate is listed
#   SELFTEST_VERBOSE=1 ...                 print each gate's output
#
# Needs the compose Postgres for the migrate scenarios (make db-up). Scratch space is
# $TMPDIR/meterwise-selftest.*; web/node_modules is symlinked, never reinstalled.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
REPO="$(pwd)"
. scripts/lib.sh
ONLY=("$@")
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/meterwise-selftest.XXXXXX")"
BASE="$SCRATCH/base" W="$SCRATCH/work" STUBS="$SCRATCH/stubs"
trap 'rm -rf "$SCRATCH"' EXIT
mkdir -p "$BASE" "$STUBS"
git ls-files -z --cached --others --exclude-standard | rsync -a --from0 --files-from=- ./ "$BASE/"
[ ! -d web/node_modules ] || ln -s "$REPO/web/node_modules" "$BASE/web/node_modules"
(cd "$BASE" && git init -q -b main && git add -A && git -c user.name=selftest -c user.email=selftest@example.test commit -q -m base)
# stubs so a "no database" scenario can never touch the real compose project
printf '#!/bin/sh\nexit 1\n' >"$STUBS/docker"; cp "$STUBS/docker" "$STUBS/pg_isready"; chmod +x "$STUBS"/*

pass=0 bad=0
want_gate() { [ "${#ONLY[@]}" -eq 0 ] && return 0; local g; for g in "${ONLY[@]}"; do [ "$g" = "$1" ] && return 0; done; return 1; }

# strip_store_inputs removes the Plan 01 store inputs and amends the scratch base commit to match.
strip_store_inputs() {
  rm -rf internal/store/migrations internal/store/queries internal/store/sqlc sqlc.yaml
  go run ./cmd/gen schema >/dev/null 2>&1
  git add -A && git -c user.name=selftest -c user.email=selftest@example.test commit -q --amend --no-edit
}

# run_case KIND GATE NAME EXPECT MUTATION [ENV...]   KIND: red|control
run_case() {
  local kind="$1" gate="$2" name="$3" expect="$4" mut="$5"; shift 5
  want_gate "$gate" || return 0
  rm -rf "$W"; cp -Rp "$BASE" "$W"
  # sqlc and migrate scenarios are written for a tree with no store inputs (the pre-Plan-01 state):
  # strip them from the base commit, so the scenarios stay meaningful. NOSTRIP=1 keeps the real inputs.
  case "$gate" in sqlc | migrate) [ -n "${NOSTRIP:-}" ] || (cd "$W" && strip_store_inputs) ;; esac
  (cd "$W" && "$mut") || { echo "✗ SETUP FAILED  [$gate] $name"; bad=$((bad + 1)); return; }
  local out rc
  out="$(cd "$W" && env -u METERWISE_BASE_REF "$@" scripts/run-checks.sh $gate 2>&1)"; rc=$?
  [ -z "${SELFTEST_VERBOSE:-}" ] || echo "$out"
  if [ -n "${POST:-}" ] && ! (cd "$W" && "$POST"); then echo "✗ POSTCHECK FAILED [$gate] $name ($POST)"; bad=$((bad + 1)); return; fi
  if [ "$kind" = red ]; then
    if [ "$rc" -ne 0 ] && grep -qF -- "$expect" <<<"$out"; then echo "✓ RED     [$gate] $name"; pass=$((pass + 1))
    else echo "✗ NOT RED [$gate] $name  (rc=$rc, wanted output containing: $expect)"; echo "$out" | tail -n 8 | sed 's/^/      /'; bad=$((bad + 1)); fi
  else
    if [ "$rc" -eq 0 ] && grep -qF -- "$expect" <<<"$out"; then echo "✓ GREEN   [$gate] $name"; pass=$((pass + 1))
    else echo "✗ NOT GREEN [$gate] $name  (rc=$rc, wanted output containing: $expect)"; echo "$out" | tail -n 8 | sed 's/^/      /'; bad=$((bad + 1)); fi
  fi
}
red() { run_case red "$@"; }
green() { run_case control "$@"; }
gofile() { mkdir -p "$(dirname "$1")"; cat >"$1"; }

nop() { :; }
# ---- fmt, vet, mod, lint
m_fmt() { printf 'package config\nfunc  Bad( ){}\n' >internal/config/bad.go; }
m_vet() { gofile internal/config/bad.go <<<'package config
import "fmt"
var S = fmt.Sprintf("%d", "x")'; }
m_mod() { sed -i.bak 's/^toolchain/require github.com\/google\/uuid v1.6.0\n\ntoolchain/' go.mod; rm -f go.mod.bak; }
m_lint() { gofile internal/config/bad.go <<<'package config
import "fmt"

// Shout prints to stdout, which library code must not do.
func Shout() {
	fmt.Println("hi")
}'; }
m_fmtsyn() { printf 'package config\nfunc (\n' >internal/config/bad.go; }
red fmt "Go file that does not parse" "does not parse" m_fmtsyn
red fmt "unformatted Go file" "not gofmt-formatted" m_fmt
red vet "printf verb mismatch" "Sprintf format" m_vet
red mod "unused requirement in go.mod" "not tidy" m_mod
red lint "fmt.Println in library code" "forbidigo" m_lint
if want_gate lint; then
  rm -rf "$SCRATCH/pl1" "$SCRATCH/pl2"; cp -Rp "$BASE" "$SCRATCH/pl1"; cp -Rp "$BASE" "$SCRATCH/pl2"
  (cd "$SCRATCH/pl1" && scripts/check-lint.sh >"$SCRATCH/pl1.out" 2>&1) & p1=$!
  (cd "$SCRATCH/pl2" && scripts/check-lint.sh >"$SCRATCH/pl2.out" 2>&1) & p2=$!
  wait "$p1"; r1=$?; wait "$p2"; r2=$?
  if [ "$r1" -eq 0 ] && [ "$r2" -eq 0 ]; then echo "✓ GREEN   [lint] two concurrent lint runs (parallel worktrees)"; pass=$((pass + 1))
  else echo "✗ NOT GREEN [lint] concurrent lint runs (rc $r1, $r2)"; tail -n 3 "$SCRATCH/pl1.out" "$SCRATCH/pl2.out"; bad=$((bad + 1)); fi
fi
green fmt "clean tree" "all gates passed" nop

# ---- arch: one seeded violation per rule, on the real repository layout
m_h2() { mkdir -p internal/api; gofile internal/api/doc.go <<<'// Package api is the control plane.
package api'; gofile internal/store/x.go <<<'// Package store is the base.
package store
import _ "github.com/ethosagent/meterwise/internal/api"'; }
# a command that can never become real (a name no plan will use): a real package's path would stop being a seed once it gets a doc.go
m_p1() { gofile cmd/zz-selftest/main.go <<<'package main
func main() {}'; }
m_h10() { gofile cmd/meterwised/zz_selftest.go <<<'package main
import _ "github.com/ethosagent/meterwise/internal/identity/clerkmock"'; }
m_h1() { gofile internal/dataplane/doc.go <<<'// Package dataplane wires tollgate.
package dataplane
import _ "github.com/ethosagent/tollgate/internal/core"'; }
m_h4() { mkdir -p internal/store/queries; echo 'INSERT INTO calls (id) VALUES ($1);' >internal/store/queries/workspaces.sql; }
m_h5() { echo "import _ from 'lodash'" >web/src/bad.ts; }
m_h3() { gofile internal/history/doc.go <<<'// Package history is pure.
package history
import _ "net/http"'; }
m_h0() { gofile internal/contract/leak.go <<<'package contract
import _ "github.com/ethosagent/meterwise/internal/config"'; }
m_h6() { gofile internal/tenancy/doc.go <<<'// Package tenancy owns orgs.
package tenancy
import _ "github.com/gin-gonic/gin"'; }
m_d1() { gofile internal/misc/doc.go <<<'// Package misc is in no layer.
package misc'; }
red arch "H0 contract imports a feature package" "H0 " m_h0
red arch "H1 imports tollgate/internal" "H1 " m_h1
red arch "H2 store imports api" "H2 " m_h2
red arch "H3 history imports net/http" "H3 " m_h3
red arch "H4 non-owner writes calls" "H4 " m_h4
red arch "H5 web imports lodash" "H5 " m_h5
red arch "H6 unlisted third-party import" "H6 " m_h6
m_h1cmd() { gofile internal/dataplane/doc.go <<<'// Package dataplane wires tollgate.
package dataplane
import _ "github.com/ethosagent/tollgate/cmd/tollgate-bench"'; }
m_h4q() { mkdir -p internal/store/queries; printf 'UPDATE ONLY "sessions" SET n = 1;\n' >internal/store/queries/workspaces.sql; }
m_h7() { gofile internal/savings/s.go <<<'//go:build offline

// Package savings is hidden from the gates by a build tag.
package savings
import _ "net/http"'; }
m_h7s() { gofile internal/config/plat_linux.go <<<'package config'; }
m_h8() { gofile internal/audit/doc.go <<<'// Package audit appends audit events.
package audit'; gofile internal/audit/net_test.go <<<'package audit
import "testing"
func TestDials(t *testing.T) {}'; }
m_h6store() { gofile internal/api/x.go <<<'// Package api is the base.
package api
import _ "github.com/go-chi/chi/v5"'; }
modclean() { git diff --quiet -- go.mod go.sum; }
POST=modclean green arch "an allowlisted import missing from go.mod does not rewrite go.mod or go.sum" "all gates passed" m_h6store
m_h9() { mkdir -p internal/config/nested; printf 'module nested\n' >internal/config/nested/go.mod; gofile internal/config/nested/n_test.go <<<'package nested
import "testing"
func TestFail(t *testing.T) { t.Fatal("hidden by a nested module") }'; }
m_dist() { gofile internal/savings/dist/doc.go <<<'// Package dist hides behind a directory name.
package dist'; gofile internal/savings/dist/x.go <<<'//go:build offline

package dist
import _ "net/http"'; }
m_h8c() { gofile internal/audit/doc.go <<<'// Package audit appends audit events.
package audit'; gofile internal/audit/net_test.go <<<'package audit
import (
	"os"
	"testing"
)
// TestMain should call nonet.Run(m) but only this comment says so.
func TestMain(m *testing.M) { os.Exit(m.Run()) }'; }
m_h4alias() { mkdir -p internal/store/queries; printf 'UPDATE calls c SET n = 1;\n' >internal/store/queries/workspaces.sql; }
red arch "H9 nested go.mod hides a package from every gate" "H9 " m_h9
red arch "H7 build tag inside a package named dist" "H7 " m_dist
red arch "H8 nonet.Run only mentioned in a comment" "H8 " m_h8c
red arch "H4 aliased UPDATE on a ledger table" "H4 " m_h4alias
red arch "H1 imports tollgate's non-public cmd package" "H1 " m_h1cmd
red arch "H4 quoted UPDATE ONLY on a ledger table" "H4 " m_h4q
red arch "H7 build tag hides a package from every gate" "H7 " m_h7
red arch "H7 GOOS filename suffix" "H7 " m_h7s
red arch "H8 package with tests but no nonet TestMain" "H8 " m_h8
red arch "D1 package in no layer" "D1 " m_d1
red arch "P1 package without doc comment" "P1 " m_p1
red arch "H10 production code imports the Clerk twin" "H10 " m_h10
m_h11() { gofile internal/api/zz_selftest.go <<<'package api
func inlineRoleCheck(role string) bool { return role == "admin" }'; }
red arch "H11 a handler decides access by comparing a role" "H11 " m_h11
m_h11t() { gofile internal/api/zz_selftest.go <<<'package api
import c "github.com/ethosagent/meterwise/internal/contract"
var adm = c.RoleAdmin
func inlineRoleCheck(r c.Role) bool { return string(r) != "" && r == adm }'; }
red arch "H11 an aliased import and a local copy do not hide a role comparison" "H11 " m_h11t
green arch "clean tree" "all gates passed" nop

# ---- test: failing test, coverage floor, visible skip, CI fail-closed
m_testfail() { gofile internal/config/bad_test.go <<<'package config_test
import "testing"
func TestBroken(t *testing.T) { t.Fatal("seeded failure") }'; }
m_cov() { { echo 'package config'; echo 'func Big(n int) int {'; for i in $(seq 1 600); do echo "	if n == $i { return $i }"; done; echo '	return 0'; echo '}'; } >internal/config/big.go; }
red test "failing test" "seeded failure" m_testfail
red test "coverage below the floor" "below 75%" m_cov
green test "no database reachable, local: skip is loud" "! SKIP test TestConfiguredDatabaseIsReachable" nop PATH="$STUBS:$PATH" TEST_DATABASE_URL= CI= COVERAGE_MIN=1
red test "no database under CI=true fails closed" "unset under CI=true" nop PATH="$STUBS:$PATH" TEST_DATABASE_URL= CI=true

# ---- vuln
m_vuln() { cat >>go.mod <<<'
require gopkg.in/yaml.v2 v2.2.2'; gofile internal/config/vuln.go <<<'package config
import "gopkg.in/yaml.v2"
var _ = yaml.Unmarshal([]byte("a: 1"), new(map[string]any))'; (go mod tidy >/dev/null 2>&1 || true); }
red vuln "called vulnerable dependency" "Vulnerability" m_vuln

# ---- docs
m_twin() { echo "drift" >>AGENTS.md; }
m_desc() { sed -i.bak "s/^description: Record an architecture decision.*/description: $(printf 'x%.0s' $(seq 1 260))/" .agents/skills/write-adr/SKILL.md; rm -f .agents/skills/write-adr/SKILL.md.bak; }
m_planlink() { echo "See [the plan](plan/00a-agent-scaffolding.md)." >>docs/orientation.md; }
m_manual() { sed -i.bak '/disable-model-invocation/d' .agents/skills/handoff/SKILL.md; rm -f .agents/skills/handoff/SKILL.md.bak; }
m_adr() { cp docs/decisions/template.md docs/decisions/0099-unlisted.md; }
m_hookx() { chmod -x .claude/hooks/read-guard.sh; }
m_lens() { sed -i.bak 's/\[streaming, privacy/[streaming, ghostlens, privacy/' .agents/pipeline.yaml; rm -f .agents/pipeline.yaml.bak; }
m_prot() { sed -i.bak 's/`internal\/archtest\/\*\*`, //' CLAUDE.md; cp CLAUDE.md AGENTS.md; rm -f CLAUDE.md.bak; }
m_todo() { printf '// TO%s: later\n' DO >>cmd/meterwised/main.go; }
red docs "CLAUDE.md and AGENTS.md differ" "CLAUDE.md and AGENTS.md differ" m_twin
red docs "skill description over 250 chars" "description is" m_desc
red docs "link into gitignored plan/" "links into plan/" m_planlink
red docs "manual-only skill without disable-model-invocation" "manual-only skill needs" m_manual
red docs "ADR missing from the index" "is not listed in docs/decisions/README.md" m_adr
red docs "hook script not executable" "does not point at an executable file" m_hookx
red docs "pipeline names a lens with no rubric" "ghostlens" m_lens
red docs "gates path missing from Protected paths" "does not list the risk.yaml gates path" m_prot
red docs "unfinished-work marker" "placeholder markers" m_todo
m_descblock() { awk 'BEGIN{b="  "; for(i=0;i<60;i++) b=b "word "} /^description:/{print "description: >-"; print b; next} {print}' .agents/skills/write-adr/SKILL.md >s.tmp && mv s.tmp .agents/skills/write-adr/SKILL.md; }
m_unprot() { sed -i.bak '/web\/biome.json/d' .agents/risk.yaml; rm -f .agents/risk.yaml.bak; }
m_dockerv() { sed -i.bak 's/^ARG GO_VERSION=.*/ARG GO_VERSION=1.20.0/' Dockerfile; rm -f Dockerfile.bak; }
red docs "description folded with >- over 250 chars" "description is" m_descblock
red docs "web/biome.json dropped from the gates tier" "resolved to" m_unprot
red docs "Dockerfile Go version differs from go.mod toolchain" "ARG GO_VERSION" m_dockerv
green docs "clean tree (plan/ absent)" "all gates passed" nop

# ---- size
m_big() { { echo 'package config'; for i in $(seq 1 801); do echo "var v$i = $i"; done; } >internal/config/huge.go; }
m_map() { { cat CLAUDE.md; for i in $(seq 1 45); do echo "line $i"; done; } >CLAUDE.md.new; mv CLAUDE.md.new CLAUDE.md; cp CLAUDE.md AGENTS.md; }
m_tsbig() { { for i in $(seq 1 401); do echo "export const v$i = $i;"; done; } >web/src/huge.ts; }
m_readme() { mkdir -p web/src/features/x; { for i in $(seq 1 121); do echo "line $i"; done; } >web/src/features/x/README.md; }
red size "Go file over 800 lines" "cap 800" m_big
red size "CLAUDE.md over 150 lines" "cap 150" m_map
red size "TS file over 400 lines" "cap 400" m_tsbig
red size "feature README over 120 lines" "cap 120" m_readme

# ---- sqlc: SKIP without inputs, real once they exist
m_sqlc_cfg() { mkdir -p internal/store/migrations internal/store/queries internal/store/sqlc
  cat >sqlc.yaml <<'Y'
version: "2"
sql:
  - engine: postgresql
    schema: internal/store/migrations
    queries: internal/store/queries
    gen:
      go:
        package: sqlc
        out: internal/store/sqlc
        sql_package: pgx/v5
Y
  printf -- '-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id uuid PRIMARY KEY, name text NOT NULL);\n-- +goose Down\nDROP TABLE orgs;\n' >internal/store/migrations/0001_orgs.sql
  printf -- '-- name: GetOrg :one\nSELECT id, name FROM orgs WHERE id = $1;\n' >internal/store/queries/orgs.sql; }
m_sqlc_noqueries() { m_sqlc_cfg; rm -rf internal/store/queries; }
m_sqlc_stale() { m_sqlc_cfg; gotool sqlc generate; echo '// hand edit' >>internal/store/sqlc/orgs.sql.go; }
m_sqlc_nogen() { m_sqlc_cfg; }
m_sqlc_ok() { m_sqlc_cfg; gotool sqlc generate; }
green sqlc "no sqlc.yaml: explicit SKIP" "! SKIP sqlc" nop
red sqlc "sqlc.yaml but no queries dir" "has no files" m_sqlc_noqueries
red sqlc "output never generated" "stale or hand-edited" m_sqlc_nogen
red sqlc "generated output hand-edited" "stale or hand-edited" m_sqlc_stale
m_sqlc_yml() { m_sqlc_cfg; git mv sqlc.yaml sqlc.yml 2>/dev/null || mv sqlc.yaml sqlc.yml; }
m_sqlc_orphan() { mkdir -p internal/store/queries; printf -- '-- name: X :one\nSELECT 1;\n' >internal/store/queries/x.sql; }
red sqlc "sqlc.yml instead of sqlc.yaml" "exactly one root sqlc.yaml" m_sqlc_yml
red sqlc "queries without any sqlc config" "no sqlc.yaml" m_sqlc_orphan
green sqlc "generated output current" "all gates passed" m_sqlc_ok

# ---- migrate (needs the compose Postgres)
m_mig_origin_ok() { mig 0001_orgs.sql "$GOOD"; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m mig; git branch -m main feature; git update-ref refs/remotes/origin/main HEAD~1; mig 0002_workspaces.sql '-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\n-- +goose Down\nDROP TABLE workspaces;\n'; }
mig() { mkdir -p internal/store/migrations; printf -- "$2" >"internal/store/migrations/$1"; }
GOOD='-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id uuid PRIMARY KEY, name text NOT NULL);\n-- +goose Down\nDROP TABLE orgs;\n'
m_mig_ok() { mig 0001_orgs.sql "$GOOD"; }
m_mig_nohdr() { mig 0001_orgs.sql '-- +goose Up\nCREATE TABLE orgs (id int);\n-- +goose Down\nDROP TABLE orgs;\n'; }
m_mig_nodown() { mig 0001_orgs.sql '-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id int);\n'; }
m_mig_leaky() { mig 0001_orgs.sql '-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id int);\n-- +goose Down\nSELECT 1;\n'; }
m_mig_badsql() { mig 0001_orgs.sql '-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id int);\n-- +goose Down\nDROP TABLE ghosts;\n'; }
m_mig_two() { mig 0001_orgs.sql "$GOOD"; mig 0002_users.sql '-- feature: users\n-- +goose Up\nCREATE TABLE users (id int);\nALTER TABLE orgs ADD COLUMN slug text;\n-- +goose Down\nALTER TABLE orgs DROP COLUMN slug;\nDROP TABLE users;\n'; }
m_mig_edit() { mig 0001_orgs.sql "$GOOD"; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m mig; sed -i.bak 's/name text NOT NULL/name text/' internal/store/migrations/0001_orgs.sql; rm -f internal/store/migrations/0001_orgs.sql.bak; }
green migrate "no migrations: explicit SKIP" "! SKIP migrate" nop
green migrate "valid migration: up, down-to 0, up" "all gates passed" m_mig_ok
red migrate "missing feature header" "first lines need" m_mig_nohdr
red migrate "missing Down section" "missing a non-empty" m_mig_nodown
red migrate "Down that does not run" "does not exist" m_mig_badsql
red migrate "Down that leaves tables behind" "Down does not restore the schema" m_mig_leaky
red migrate "one migration touching two features" "owned by feature orgs" m_mig_two
red migrate "applied migration edited" "applied migrations were modified" m_mig_edit
m_mig_db() { mkdir -p db/migrations; printf -- "$GOOD" >db/migrations/0001_orgs.sql; }
m_mig_sub() { mkdir -p internal/store/migrations/old; printf -- "$GOOD" >internal/store/migrations/old/0001_orgs.sql; }
m_mig_upper() { mkdir -p internal/store/migrations; printf -- "$GOOD" >internal/store/migrations/0001_orgs.SQL; }
m_mig_misc() { mig 0001_misc.sql '-- feature: misc\n-- +goose Up\nCREATE TABLE orgs (id int);\nCREATE TABLE workspaces (id int);\nCREATE TABLE audit_log (id int);\n-- +goose Down\nDROP TABLE audit_log;\nDROP TABLE workspaces;\nDROP TABLE orgs;\n'; }
m_mig_quoted() { mig 0001_orgs.sql "$GOOD"; mig 0002_workspaces.sql '-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\nALTER TABLE "orgs" ADD COLUMN x int;\n-- +goose Down\nALTER TABLE orgs DROP COLUMN x;\nDROP TABLE workspaces;\n'; }
m_mig_trigger() { mig 0001_orgs.sql "$GOOD"; mig 0002_workspaces.sql '-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\nCREATE TRIGGER t BEFORE INSERT ON orgs FOR EACH ROW EXECUTE FUNCTION f();\n-- +goose Down\nDROP TABLE workspaces;\n'; }
m_mig_noopdown() { mig 0001_orgs.sql "$GOOD"; mig 0002_orgs.sql '-- feature: orgs\n-- +goose Up\nALTER TABLE orgs ADD COLUMN a int;\n-- +goose Down\nSELECT 1;\n'; }
m_mig_origin() { mig 0001_orgs.sql "$GOOD"; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m mig; git update-ref refs/remotes/origin/main HEAD; git branch -m main feature; sed -i.bak 's/name text NOT NULL/name text/' internal/store/migrations/0001_orgs.sql; rm -f internal/store/migrations/0001_orgs.sql.bak; }
m_mig_nobase() { mig 0001_orgs.sql "$GOOD"; git branch -m main feature; }
green migrate "origin/main fallback: new migration on a PR branch is fine" "all gates passed" m_mig_origin_ok
m_mig_seq() { mig 0001_orgs.sql "$GOOD"; mig 0002_orgs.sql '-- feature: orgs\n-- +goose Up\nCREATE SEQUENCE orgs_seq;\n-- +goose Down\nSELECT 1;\n'; }
m_mig_enum() { mig 0001_orgs.sql '-- feature: orgs\n-- +goose Up\nCREATE TYPE org_kind AS ENUM ('"'a'"');\nCREATE TABLE orgs (id uuid PRIMARY KEY, kind org_kind);\n-- +goose Down\nDROP TABLE orgs;\nDROP TYPE org_kind;\n'; mig 0002_orgs.sql '-- feature: orgs\n-- +goose Up\nALTER TYPE org_kind ADD VALUE '"'b'"';\n-- +goose Down\nSELECT 1;\n'; }
m_mig_grant() { mig 0001_orgs.sql "$GOOD"; mig 0002_orgs.sql '-- feature: orgs\n-- +goose Up\nGRANT SELECT ON orgs TO PUBLIC;\n-- +goose Down\nSELECT 1;\n'; }
m_mig_prec() { mig 0001_orgs.sql '-- feature: orgs\n-- +goose Up\nCREATE TABLE orgs (id int, price numeric(14,6));\n-- +goose Down\nDROP TABLE orgs;\n'; mig 0002_orgs.sql '-- feature: orgs\n-- +goose Up\nALTER TABLE orgs ALTER COLUMN price TYPE numeric(20,2);\n-- +goose Down\nSELECT 1;\n'; }
m_mig_alias() { mig 0001_orgs.sql "$GOOD"; mig 0002_workspaces.sql '-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\nUPDATE orgs o SET name = $$x$$;\n-- +goose Down\nDROP TABLE workspaces;\n'; }
m_mig_launder() { mig 0001_workspaces.sql '-- feature: workspaces\n-- +goose Up\nCREATE TABLE workspaces (id int);\nCREATE TABLE audit_log (id int);\nCREATE TABLE calls (id int);\nCREATE TABLE orgs (id int);\n-- +goose Down\nDROP TABLE orgs;\nDROP TABLE calls;\nDROP TABLE audit_log;\nDROP TABLE workspaces;\n'; printf 'workspaces audit_log calls orgs\n' >internal/store/migration-families.txt; }
m_mig_deleted() { mig 0001_orgs.sql "$GOOD"; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m mig; rm internal/store/migrations/0001_orgs.sql; }
m_mig_order() { mig 0002_orgs.sql "$GOOD"; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m mig; git checkout -q -b feature; echo x >unrelated.txt; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m other; mig 0001_users.sql '-- feature: users\n-- +goose Up\nCREATE TABLE users (id int);\n-- +goose Down\nDROP TABLE users;\n'; }
red migrate "numeric(14,6) to numeric(20,2) with a no-op Down" "Down does not restore the schema" m_mig_prec
red migrate "aliased UPDATE of another feature's table" "touches table orgs" m_mig_alias
red migrate "manifest line that launders a four-table migration" "ledger table audit_log may only belong" m_mig_launder
red migrate "every applied migration deleted" "applied migrations were deleted" m_mig_deleted
red migrate "new migration numbered below the newest applied one" "must use a higher number" m_mig_order
red migrate "TEST_DATABASE_URL with a percent-encoded host parameter" "not allowed" m_mig_ok TEST_DATABASE_URL='postgres://u:p@127.0.0.1:54329/x?h%6Fst=db.example.com'
red migrate "TEST_DATABASE_URL with a dbname override" "not allowed" m_mig_ok TEST_DATABASE_URL='postgres://u:p@127.0.0.1:54329/x?dbname=real'
red migrate "SQL in db/migrations" "SQL outside" m_mig_db
red migrate "SQL in a migrations subdirectory" "SQL outside" m_mig_sub
red migrate "upper-case .SQL migration" "SQL outside" m_mig_upper
red migrate "generic feature name holding three tables" "too generic" m_mig_misc
red migrate "quoted ALTER of another feature's table" "owned by feature orgs" m_mig_quoted
red migrate "trigger on another feature's table" "owned by feature orgs" m_mig_trigger
red migrate "Down that leaves a sequence behind" "Down does not restore the schema" m_mig_seq
red migrate "Down that leaves an enum value behind" "Down does not restore the schema" m_mig_enum
red migrate "Down that leaves a grant behind" "Down does not restore the schema" m_mig_grant
red migrate "Down that is a no-op" "Down does not restore the schema" m_mig_noopdown
red migrate "applied migration edited, only origin/main exists" "applied migrations were modified" m_mig_origin
m_mig_lastcommit() { mig 0001_orgs.sql "$GOOD"; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m mig; sed -i.bak 's/name text NOT NULL/name text/' internal/store/migrations/0001_orgs.sql; rm -f internal/store/migrations/0001_orgs.sql.bak; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m edit; }
red migrate "applied migration edited in the last commit on main itself" "applied migrations were modified" m_mig_lastcommit
red migrate "no main or origin/main under CI=true" "cannot prove applied migrations are unchanged" m_mig_nobase CI=true
m_mig_baseref() { mig 0001_orgs.sql "$GOOD"; git add -A; git -c user.name=t -c user.email=t@example.test commit -q -m mig; git branch trunk; git branch -m main feature; sed -i.bak 's/name text NOT NULL/name text/' internal/store/migrations/0001_orgs.sql; rm -f internal/store/migrations/0001_orgs.sql.bak; }
red migrate "METERWISE_BASE_REF names the base: edited applied migration" "applied migrations were modified" m_mig_baseref METERWISE_BASE_REF=trunk
red migrate "METERWISE_BASE_REF that does not resolve fails closed" "does not resolve" m_mig_baseref METERWISE_BASE_REF=no-such-branch
m_mig_emptybadref() { mkdir -p internal/store/migrations; }
red migrate "unresolvable METERWISE_BASE_REF with an empty migrations dir fails closed, not SKIP" "does not resolve" m_mig_emptybadref METERWISE_BASE_REF=no-such-branch
green migrate "empty migrations dir with no base ref is still the explicit SKIP" "! SKIP migrate" m_mig_emptybadref
red migrate "TEST_DATABASE_URL on a remote host is refused" "not loopback" m_mig_ok TEST_DATABASE_URL=postgres://u:p@db.example.com:5432/x
red migrate "TEST_DATABASE_URL without a database name is refused" "no database name" m_mig_ok TEST_DATABASE_URL=postgres://u:p@127.0.0.1:54329
red migrate "migrations but no Postgres: fails closed" "cannot create a throwaway database" m_mig_ok TEST_DATABASE_URL=postgres://x:x@127.0.0.1:1/x

# ---- web
m_any() { printf 'export const x: any = 1;\n' >web/src/bad.ts; }
m_tsc() { printf 'export const n: number = "text";\n' >web/src/bad.ts; }
m_vitest() { printf 'import { expect, it } from "vitest";\n\nit("fails", () => {\n  expect(1).toBe(2);\n});\n' >web/src/bad.test.ts; }
m_webcov() { { for i in $(seq 1 60); do printf 'export function f%s(a: number): number {\n  if (a > %s) {\n    return a * 2;\n  }\n  return a;\n}\n' "$i" "$i"; done; } >web/src/untested.ts; }
m_barrel() { printf 'export const a = 1;\n' >web/src/index.ts; }
m_barrelmts() { printf 'export const a = 1;\n' >web/src/index.mts; }
m_nofeat() { mkdir -p web/src/features/billing; printf 'export const b = 1;\n' >web/src/features/billing/Page.ts; }
m_h5web() { printf 'import _ from "lodash";\nexport const l = _;\n' >web/src/bad.ts; }
m_pkg() { sed -i.bak 's/"devDependencies": {/"devDependencies": {\n    "left-pad": "1.3.0",/' web/package.json; rm -f web/package.json.bak; }
m_webnopkg() { git rm -q web/package.json; }
red web "web/package.json deleted while web/src exists" "web/package.json is missing" m_webnopkg
red web "unresolvable METERWISE_BASE_REF with web/package.json deleted fails closed" "does not resolve" m_webnopkg METERWISE_BASE_REF=no-such-branch
red web "explicit any" "npm run check failed" m_any
red web "type error" "npm run typecheck failed" m_tsc
red web "failing vitest" "npm run test failed" m_vitest
red web "coverage below 70 %" "npm run test failed" m_webcov
m_webcov_testdir() { mkdir -p web/src/test; { for i in $(seq 1 60); do printf 'export function t%s(a: number): number {\n  if (a > %s) {\n    return a * 2;\n  }\n  return a;\n}\n' "$i" "$i"; done; } >web/src/test/untested.ts; }
red web "untested file in web/src/test is not exempt from coverage" "npm run test failed" m_webcov_testdir
m_webcov_contract() { { for i in $(seq 1 60); do printf 'export function c%s(a: number): number {\n  if (a > %s) {\n    return a * 2;\n  }\n  return a;\n}\n' "$i" "$i"; done; } >web/src/contract/handwritten.ts; }
red web "untested file next to generated.ts is not exempt from coverage" "npm run test failed" m_webcov_contract
m_hex_testdir() { mkdir -p web/src/test; printf 'export const c = "#ff0000";\n' >web/src/test/extra.ts; }
red web "hex colour in a new web/src/test file is caught by the hygiene test" "npm run test failed" m_hex_testdir
red web "index.ts barrel" "barrels are banned" m_barrel
red web "index.mts barrel" "barrels are banned" m_barrelmts
red web "feature folder without README" "has no README.md" m_nofeat
red arch "web import outside the allowlist (H5 lives in the arch gate)" "H5" m_h5web
red arch "web package.json declares an unlisted package (H5)" "left-pad" m_pkg
green web "clean tree" "all gates passed" nop

# ---- generated
m_genedit() { echo 'export const sneaky = 1;' >>web/src/contract/generated.ts; }
m_genroute() { sed -i.bak 's|^var routes = \[\]Route{|var routes = []Route{\n\t{Method: "GET", Pattern: "/api/v1/ghost", Public: true, Summary: "x", Response: "Health"},|' internal/contract/routes.go; rm -f internal/contract/routes.go.bak; }
m_gendel() { rm internal/api/openapi.json; }
m_genschema() { mig 0001_orgs.sql "$GOOD"; }
red generated "hand-edited TS twin" "stale or was edited by hand" m_genedit
red generated "route added, twins not regenerated" "stale or was edited by hand" m_genroute
m_gentax() { echo 'export const sneaky = 1;' >>web/src/access/taxonomy.ts; }
red generated "hand-edited taxonomy twin is stale" "stale" m_gentax
m_gentaxdel() { rm internal/access/taxonomy.go; }
red generated "deleted taxonomy source fails closed" "undefined:" m_gentaxdel
red generated "openapi.json deleted" "is missing" m_gendel
red generated "migration added, docs/schema.md not regenerated" "docs/schema.md is stale" m_genschema
green generated "clean tree" "all gates passed" nop

# ---- Plan 01 inputs exist: the sqlc and migrate gates are live on the real tree (never a silent SKIP)
NOSTRIP=1 green sqlc "real tree: sqlc.yaml, queries and generated output are checked" "all gates passed" nop
NOSTRIP=1 green migrate "real tree: 0001_store applies, reverses and re-applies" "all gates passed" nop
m_sqlc_real_stale() { echo '// stale' >>internal/store/sqlc/store.sql.go; }
NOSTRIP=1 red sqlc "real tree: hand-edited sqlc output" "stale or hand-edited" m_sqlc_real_stale

echo "--- selftest: $pass as expected, $bad not"
[ "$bad" -eq 0 ]
