#!/usr/bin/env bash
# risk-tier.sh PATH... — print the risk tier (critical|gates|normal|low) of the
# change set: the HIGHEST tier any path hits in .agents/risk.yaml
# (critical > gates > normal > low). An unmatched path is `normal`.
# `*` stays inside one path segment, `**` crosses directories. With no
# arguments it reads the paths from `git diff --name-only HEAD`.
set -uo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
yaml=.agents/risk.yaml

glob_to_regex() {
  local g="$1" out="" c
  while [ -n "$g" ]; do
    case "$g" in
      '**/'*) out+='(.*/)?'; g="${g#'**/'}" ;;
      '**'*)  out+='.*'; g="${g#'**'}" ;;
      '*'*)   out+='[^/]*'; g="${g#'*'}" ;;
      *) c="${g:0:1}"; g="${g:1}"
         case "$c" in [.+?^\$\(\)\[\]\{\}\|\\]) out+="\\$c" ;; *) out+="$c" ;; esac ;;
    esac
  done
  printf '^%s$' "$out"
}

tier_of() {
  local path="$1" tier line glob
  for tier in critical gates normal low; do
    while IFS= read -r glob; do
      [ -n "$glob" ] || continue
      if [[ $path =~ $(glob_to_regex "$glob") ]]; then echo "$tier"; return; fi
    done < <(awk -v t="$tier" '
      /^tiers:/ {in_t=1; next}
      in_t && /^  [a-z]+:/ {cur=$1; sub(":","",cur); next}
      in_t && cur==t && /^    - / {
        line=$0; sub(/^    - /,"",line); sub(/[ \t]+#.*$/,"",line); gsub(/^"|"$|^'"'"'|'"'"'$/,"",line); print line }' "$yaml")
  done
  echo normal
}

[ "$#" -gt 0 ] || set -- $(git diff --name-only HEAD 2>/dev/null)
[ "$#" -gt 0 ] || { echo low; exit 0; }
best=low
rank() { case "$1" in critical) echo 4 ;; gates) echo 3 ;; normal) echo 2 ;; *) echo 1 ;; esac; }
for p in "$@"; do
  t="$(tier_of "$p")"
  [ "$(rank "$t")" -le "$(rank "$best")" ] || best="$t"
done
echo "$best"
