#!/usr/bin/env bash
# Proves the Claude hooks behave, by running the hook scripts directly with sample
# tool-call JSON on stdin (no Claude session needed, nothing is enabled globally).
# Works on a scratch copy of the tree, so a failing Stop gate or a mangled file never
# touches the real working tree.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
REPO="$(pwd)"
SCRATCH="$(mktemp -d "${TMPDIR:-/tmp}/meterwise-hooks.XXXXXX")"
P="$SCRATCH/p"
trap 'rm -rf "$SCRATCH"; [ -z "${SRV_PID:-}" ] || kill "$SRV_PID" 2>/dev/null' EXIT
mkdir -p "$P"
git ls-files -z --cached --others --exclude-standard | rsync -a --from0 --files-from=- ./ "$P/"
ln -s "$REPO/web/node_modules" "$P/web/node_modules"
(cd "$P" && git init -q -b main && git add -A && git -c user.name=t -c user.email=t@example.test commit -q -m base)
H="$P/.claude/hooks"
pass=0 bad=0
# t NAME WANT_RC WANT_TEXT -- cmd...   (stdin is passed through; combined output is checked)
t() {
  local name="$1" want_rc="$2" want="$3"; shift 4
  local out rc
  out="$("$@" 2>&1)"; rc=$?
  if [ "$rc" -eq "$want_rc" ] && grep -qF -- "$want" <<<"$out"; then echo "✓ $name"; pass=$((pass + 1))
  else echo "✗ $name (rc=$rc want $want_rc; wanted: $want)"; echo "$out" | head -n 6 | sed 's/^/      /'; bad=$((bad + 1)); fi
}
run_in() { (cd "$P" && CLAUDE_PROJECT_DIR="$P" "$@"); }
json() { printf '%s' "$1"; }
read_hook() { json "{\"tool_input\":{\"file_path\":\"$1\"$2}}" | run_in "$H/read-guard.sh"; }
bash_hook() { jq -n --arg c "$1" '{tool_input:{command:$c}}' | run_in "$H/quiet-guard.sh"; }
edit_hook() { json "{\"tool_input\":{\"file_path\":\"$P/$1\"}}" | run_in "$H/post-edit.sh"; }

# ---- read-guard
seq 1 450 >"$P/big.txt"; seq 1 50 >"$P/small.txt"
t "read-guard blocks a whole-file read over 400 lines" 2 "has 450 lines" -- read_hook "$P/big.txt" ""
t "read-guard lets a ranged read through" 0 "" -- read_hook "$P/big.txt" ',"offset":10,"limit":50'
t "read-guard lets a small file through" 0 "" -- read_hook "$P/small.txt" ""
mkdir -p "$P/web/dist"; echo x >"$P/web/dist/a.js"
t "read-guard blocks web/dist even when ranged" 2 "generated or a lockfile" -- read_hook "$P/web/dist/a.js" ',"limit":5'
t "read-guard blocks generated.ts" 2 "generated or a lockfile" -- read_hook "$P/web/src/contract/generated.ts" ""
t "read-guard blocks package-lock.json" 2 "generated or a lockfile" -- read_hook "$P/web/package-lock.json" ',"limit":5'
echo x >"$P/orgs.sql.go"
t "read-guard blocks *.sql.go" 2 "generated or a lockfile" -- read_hook "$P/orgs.sql.go" ""
t "read-guard blocks openapi.json" 2 "generated or a lockfile" -- read_hook "$P/internal/api/openapi.json" ""

# ---- read-guard reader mode (fake OpenAI-compatible server on loopback)
python3 -I - "$SCRATCH/port" <<'PY' &
import http.server, json, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("content-length", 0)); body = json.loads(self.rfile.read(n))
        open(sys.argv[1] + ".seen", "a").write(body["messages"][1]["content"][:20] + "\n")
        out = json.dumps({"choices": [{"message": {"content": "SUMMARY: counts to 450"}}]}).encode()
        self.send_response(200); self.send_header("content-type", "application/json"); self.end_headers(); self.wfile.write(out)
    def log_message(self, *a): pass
s = http.server.HTTPServer(("127.0.0.1", 0), H); open(sys.argv[1], "w").write(str(s.server_port)); s.serve_forever()
PY
SRV_PID=$!
for _ in $(seq 1 50); do [ -s "$SCRATCH/port" ] && break; sleep 0.1; done
export METERWISE_READER_URL="http://127.0.0.1:$(cat "$SCRATCH/port")"
t "reader mode returns the local model's summary" 2 "SUMMARY: counts to 450" -- read_hook "$P/big.txt" ""
seq 1 450 >"$P/prod.env"
rm -f "$SCRATCH/port.seen"
read_hook "$P/prod.env" "" >/dev/null 2>&1
if [ ! -e "$SCRATCH/port.seen" ]; then echo "✓ reader mode never sends .env files"; pass=$((pass + 1)); else echo "✗ reader mode sent a .env file"; bad=$((bad + 1)); fi
unset METERWISE_READER_URL
t "reader mode is off by default (plain refusal)" 2 "Use Grep" -- read_hook "$P/big.txt" ""

# ---- quiet-guard
t "quiet-guard blocks raw go test and names scripts/quiet.sh" 2 "scripts/quiet.sh go test ./..." -- bash_hook "go test ./..."
t "quiet-guard blocks raw golangci-lint" 2 "scripts/quiet.sh" -- bash_hook "golangci-lint run"
t "quiet-guard blocks npm test" 2 "scripts/quiet.sh" -- bash_hook "cd web && npm test"
t "quiet-guard blocks npx vitest" 2 "scripts/quiet.sh" -- bash_hook "npx vitest run"
t "quiet-guard blocks biome" 2 "scripts/quiet.sh" -- bash_hook "npx biome check ."
t "quiet-guard allows scripts/quiet.sh" 0 "" -- bash_hook "scripts/quiet.sh go test ./..."
t "quiet-guard allows run-checks" 0 "" -- bash_hook "scripts/run-checks.sh test"
t "quiet-guard allows a piped go test" 0 "" -- bash_hook "go test ./... | tail -n 20"
t "quiet-guard ignores unrelated commands" 0 "" -- bash_hook "ls -la"

# ---- post-edit
printf 'package config\nfunc  Messy( ){}\n' >"$P/internal/config/messy.go"
t "post-edit gofmt-formats a Go file" 0 "" -- edit_hook internal/config/messy.go
grep -q 'func Messy() {}' "$P/internal/config/messy.go" && { echo "✓ ...and the file really changed"; pass=$((pass + 1)); } || { echo "✗ messy.go not formatted"; bad=$((bad + 1)); }
printf 'package config\nfunc (\n' >"$P/internal/config/broken.go"
t "post-edit reports a Go syntax error" 2 "gofmt could not parse" -- edit_hook internal/config/broken.go
printf 'export const   a  =  {b:1}\n' >"$P/web/src/messy.ts"
t "post-edit biome-formats a TS file" 0 "" -- edit_hook web/src/messy.ts
grep -q 'export const a = { b: 1 };' "$P/web/src/messy.ts" && { echo "✓ ...and the TS file really changed"; pass=$((pass + 1)); } || { echo "✗ messy.ts not formatted"; bad=$((bad + 1)); }
printf 'export const   x  =  1\n' >>"$P/web/src/contract/generated.ts"; before="$(md5 -q "$P/web/src/contract/generated.ts" 2>/dev/null || md5sum "$P/web/src/contract/generated.ts" | cut -d' ' -f1)"
edit_hook web/src/contract/generated.ts >/dev/null 2>&1
after="$(md5 -q "$P/web/src/contract/generated.ts" 2>/dev/null || md5sum "$P/web/src/contract/generated.ts" | cut -d' ' -f1)"
[ "$before" = "$after" ] && { echo "✓ post-edit leaves generated.ts alone"; pass=$((pass + 1)); } || { echo "✗ post-edit rewrote generated.ts"; bad=$((bad + 1)); }
echo "# changed in CLAUDE" >>"$P/CLAUDE.md"
t "post-edit syncs CLAUDE.md to AGENTS.md" 0 "" -- edit_hook CLAUDE.md
cmp -s "$P/CLAUDE.md" "$P/AGENTS.md" && { echo "✓ ...twins are byte-identical"; pass=$((pass + 1)); } || { echo "✗ twins differ"; bad=$((bad + 1)); }
{ echo 'package config'; for i in $(seq 1 801); do echo "var w$i = $i"; done; } >"$P/internal/config/huge.go"
t "post-edit reports a file over its hard cap" 2 "cap 800" -- edit_hook internal/config/huge.go
rm -f "$P/internal/config/huge.go" "$P/internal/config/broken.go"

# ---- stop-gate (fast gates on a dirty tree)
run_in git checkout -q -- . 2>/dev/null; (cd "$P" && git clean -fdq -e web/node_modules)
t "stop-gate passes on a clean tree" 0 "" -- run_in "$H/stop-gate.sh" <<<'{}'
echo "drift" >>"$P/AGENTS.md"
t "stop-gate blocks a dirty tree that fails a fast gate" 2 "fast gates failed" -- run_in "$H/stop-gate.sh" <<<'{}'
t "stop-gate does not loop when already active" 0 "" -- run_in "$H/stop-gate.sh" <<<'{"stop_hook_active":true}'
(cd "$P" && git checkout -q -- .)
echo "A harmless extra line." >>"$P/docs/orientation.md"
t "stop-gate passes a dirty tree whose fast gates are green" 0 "" -- run_in "$H/stop-gate.sh" <<<'{}'
(cd "$P" && git checkout -q -- .)

# ---- session-start
ss() { json "{\"source\":\"$1\"}" | run_in "$H/session-start.sh"; }
t "session-start prints the branch" 0 "branch=main" -- ss startup
t "session-start works with plan/ absent" 0 "meterwise session" -- ss startup
mkdir -p "$P/plan"; echo "# plan" >"$P/plan/00a-x.md"; echo "# plan" >"$P/plan/01-y.md"; touch -t 202601010000 "$P/plan/00a-x.md"
t "session-start prints the newest plan" 0 "Active plan: plan/01-y.md" -- ss startup
printf 'Goal: finish the thing\n' >"$P/HANDOFF.md"
t "session-start prints HANDOFF.md" 0 "Goal: finish the thing" -- ss resume
t "session-start gives the model/effort reminder once, on startup" 0 "pick the model and effort" -- ss startup
out="$(ss resume 2>&1)"; grep -q "pick the model" <<<"$out" && { echo "✗ reminder repeated on resume"; bad=$((bad + 1)); } || { echo "✓ no reminder on resume"; pass=$((pass + 1)); }
t "session-start restates the non-negotiables after compaction" 0 "Client-first streaming" -- ss compact

echo "--- selftest-hooks: $pass ok, $bad failed"
[ "$bad" -eq 0 ]
