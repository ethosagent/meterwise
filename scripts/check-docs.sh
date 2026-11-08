#!/usr/bin/env bash
# Gate: the agent-facing docs, skills, hooks and harness contract stay true and small
# (Context Budget Playbook). Checks:
#   1. CLAUDE.md and AGENTS.md are byte-identical (size caps live in check-size.sh).
#   2. Every skill: frontmatter name == directory, description <= 250 chars, an executable
#      scripts/*.sh that parses; manual-only skills carry disable-model-invocation: true.
#   3. .claude/skills is a symlink to ../.agents/skills; .claude/agents/*.md have frontmatter,
#      and reader.md sets autoCompactWindow.
#   4. Every ADR is listed in docs/decisions/README.md.
#   5. Relative markdown links resolve, and none points into gitignored plan/ (ruling R1).
#   6. Review lenses: pipeline.yaml names only existing lenses, the README lists them all,
#      TNT and DATA have at least 6 checks each.
#   7. risk.yaml resolves sample paths to the right tier; every gates glob is in the map's
#      "Protected paths" section.
#   8. settings.json is valid JSON and every hook command exists and is executable.
#   9. The required runbooks exist; no unfinished-work markers in shipped files.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
. scripts/lib.sh
fail=0
err() { echo "✗ $*"; fail=1; }

cmp -s CLAUDE.md AGENTS.md || err "CLAUDE.md and AGENTS.md differ. Fix: edit one, then cp CLAUDE.md AGENTS.md (or the reverse)."

for s in .agents/skills/*/SKILL.md; do
  d="${s%/SKILL.md}"; name="${d##*/}"
  # one pass over the frontmatter: prints "<first line>\t<name>\t<description>\t<manual flag>"
  IFS=$'\t' read -r first fm_name desc manual < <(awk 'NR==1{f=$0} /^---$/{n++; next}
    n==1 && /^name:/{sub(/^name:[ ]*/,""); nm=$0}
    n==1 && /^description:/{sub(/^description:[ ]*/,""); ds=$0}
    n==1 && /^disable-model-invocation:[ ]*true/{mn="true"}
    END{printf "%s\t%s\t%s\t%s\n", f, nm, ds, (mn=="" ? "false" : mn)}' "$s")
  [ "$first" = "---" ] || { err "$s: missing frontmatter"; continue; }
  [ "$fm_name" = "$name" ] || err "$s: frontmatter name '$fm_name' != directory '$name'"
  [ -n "$desc" ] || err "$s: empty description"
  [ "${#desc}" -le "$MAX_SKILL_DESC" ] || err "$s: description is ${#desc} chars (cap $MAX_SKILL_DESC)"
  for m in "${MANUAL_SKILLS[@]}"; do
    [ "$m" != "$name" ] || [ "$manual" = true ] || err "$s: manual-only skill needs 'disable-model-invocation: true'"
  done
  scripts=("$d"/scripts/*.sh)
  [ -e "${scripts[0]}" ] || err "$d: no scripts/*.sh (mechanical steps ship as scripts)"
  for sc in "${scripts[@]}"; do
    [ ! -e "$sc" ] || { [ -x "$sc" ] || err "$sc is not executable"; bash -n "$sc" 2>/dev/null || err "$sc has a shell syntax error"; }
  done
done

[ "$(readlink .claude/skills)" = "../.agents/skills" ] || err ".claude/skills must be a symlink to ../.agents/skills"
for a in .claude/agents/*.md; do
  [ "$(sed -n 1p "$a")" = "---" ] || err "$a: missing frontmatter"
  grep -q "^name: $(basename "$a" .md)$" "$a" || err "$a: frontmatter name must equal the file name"
done
grep -q '^autoCompactWindow:' .claude/agents/reader.md 2>/dev/null || err ".claude/agents/reader.md must set autoCompactWindow"

for adr in docs/decisions/[0-9][0-9][0-9][0-9]-*.md; do
  grep -qF "$(basename "$adr")" docs/decisions/README.md || err "$adr is not listed in docs/decisions/README.md"
done

while IFS= read -r f; do
  dir="${f%/*}"; [ "$dir" = "$f" ] && dir=.
  while IFS= read -r link; do
    target="${link%%#*}"
    [ -z "$target" ] && continue
    case "$target" in http://*|https://*|mailto:*) continue ;; esac
    case "$target" in plan/* | */plan/*) err "$f links into plan/, which is gitignored and absent in fresh clones: $target"; continue ;; esac
    [ -e "$dir/$target" ] || err "$f links to missing $target"
  done < <(grep -oE '\]\([^) ]+\)' "$f" | sed -E 's/^\]\(//; s/\)$//') # one grep+sed per file
done < <(list_files '*.md')

# review lenses
lenses="$(awk '/^reviews:/{r=1} r && /^    [a-z]+: *\[/ {sub(/#.*/,""); sub(/^[^\[]*\[/,""); sub(/\].*/,""); gsub(/,/," "); for(i=1;i<=NF;i++) print $i}' .agents/pipeline.yaml | sort -u)"
[ -n "$lenses" ] || err ".agents/pipeline.yaml names no review lenses"
for l in $lenses; do
  [ -f ".agents/reviews/$l.md" ] || err "pipeline.yaml names lens '$l' but .agents/reviews/$l.md is missing"
  grep -qF "[\`$l.md\`]" .agents/reviews/README.md || err ".agents/reviews/README.md does not list '$l'"
done
for pair in tenancy-authz:TNT data-integrity:DATA; do
  f=".agents/reviews/${pair%%:*}.md"; id="${pair##*:}"
  n="$(grep -cE "^\*\*$id-[0-9]+ " "$f" 2>/dev/null || true)"
  [ "${n:-0}" -ge 6 ] || err "$f has ${n:-0} $id-* checks (need at least 6)"
done

# risk tiers resolve correctly
want_tier() { got="$(scripts/risk-tier.sh "$1")"; [ "$got" = "$2" ] || err "risk-tier: $1 resolved to '$got', want '$2'"; }
want_tier internal/dataplane/forward.go critical
want_tier internal/access/evaluator.go critical
want_tier scripts/check-test.sh gates
want_tier internal/store/migrations/0001_orgs.sql gates
want_tier internal/tenancy/workspaces.go normal
want_tier docs/orientation.md low
[ "$(scripts/risk-tier.sh docs/a.md internal/ledger/x.go)" = critical ] || err "risk-tier: highest tier must win"
protected="$(awk '/^## Protected paths/{p=1; next} /^## /{p=0} p' CLAUDE.md)"
while IFS= read -r glob; do
  [ -n "$glob" ] || continue
  g="${glob%/\*\*}"
  grep -qF -- "$g" <<<"$protected" || err "CLAUDE.md Protected paths does not list the risk.yaml gates path '$glob'"
done < <(awk '/^tiers:/{t=1} t && /^  [a-z]+:/{cur=$1} t && cur=="gates:" && /^    - /{l=$0; sub(/^    - /,"",l); sub(/[ \t]+#.*$/,"",l); print l}' .agents/risk.yaml)

# settings and hooks
if command -v jq >/dev/null 2>&1; then
  jq -e . .claude/settings.json >/dev/null 2>&1 || err ".claude/settings.json is not valid JSON"
  while IFS= read -r cmd; do
    path="${cmd//\"\$CLAUDE_PROJECT_DIR\"\//}"
    [ -x "$path" ] || err "settings.json hook '$cmd' does not point at an executable file"
  done < <(jq -r '.. | objects | select(.type? == "command") | .command' .claude/settings.json)
else
  skip docs/hooks "jq not installed; cannot validate .claude/settings.json"
fi

for rb in local-dev release rotate-keys parallel-sessions model-policy agent-hygiene; do
  [ -f "docs/runbooks/$rb.md" ] || err "docs/runbooks/$rb.md is missing"
done

placeholder="TO""DO|FIX""ME"
hits="$(list_files '*.go' '*.ts' '*.tsx' '*.sh' '*.md' '*.yml' '*.yaml' | grep -v -e '^internal/archtest/' | xargs grep -nE "$placeholder" 2>/dev/null | head -n 5)"
[ -z "$hits" ] || err "placeholder markers in shipped files (finish the work or remove the note):
$hits"

exit "$fail"
