#!/usr/bin/env bash
# PreToolUse(Read): block whole-file reads of large or generated text files so one
# Read does not eat the context budget. Reads with an offset or limit pass, except
# for generated/lock files, which are never worth reading (use the source instead).
# Exit 2 blocks the call and shows stderr to the agent.
#
# Optional reader mode: when METERWISE_READER_URL is set (an OpenAI-compatible
# server, for example a local model), a blocked large-file read is summarised by
# that model and the summary is returned in the refusal. Default off. The URL must be
# loopback, and secret-like files (by name or content) are never sent. METERWISE_READER_MODEL names the model (default "local").
set -euo pipefail
MAX_LINES="${READ_GUARD_MAX_LINES:-400}"

# reader_ok FILE: may FILE be sent to METERWISE_READER_URL? Only a loopback reader, and
# never a secret-looking file (by name or by content).
reader_ok() {
  local f="$1"
  # lib.sh is sourced only here, so the common (reader mode off) path stays fast
  . "$(dirname "${BASH_SOURCE[0]}")/../../scripts/lib.sh"
  case "$METERWISE_READER_URL" in http://* | https://*) ;; *) return 1 ;; esac
  is_loopback_host "$(url_host "$METERWISE_READER_URL")" || return 1
  case "$METERWISE_READER_URL" in *@*) return 1 ;; esac
  case "$(basename "$f" | tr '[:upper:]' '[:lower:]')" in
    *.env | .env* | *.pem | *.key | *.p12 | *.pfx | *.jks | *.keystore | *.kdbx | *.tfvars | *.tfstate | \
    id_rsa* | id_dsa* | id_ecdsa* | id_ed25519* | .netrc | .npmrc | .pypirc | .pgpass | .git-credentials | \
    *secret* | *credential* | *token* | *passwd* | *password* | *apikey* | *api_key* | *auth*.json) return 1 ;;
  esac
  case "$f" in */.ssh/* | */.aws/* | */.gnupg/* | */.kube/* | */.docker/*) return 1 ;; esac
  # a process substitution, not a pipe: `head | grep -q` under pipefail reports SIGPIPE (141) on large
  # files and would let a match slip through as "no secret"
  if grep -qaiE 'BEGIN [A-Z ]*PRIVATE KEY|["'"'"']?(api[_-]?key|secret|passw(or)?d|access[_-]?token|auth[_-]?token|client[_-]?secret)["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9/+_.-]{8,}|sk-[A-Za-z0-9_-]{16,}|AKIA[0-9A-Z]{16}|xox[abpr]-[A-Za-z0-9-]{10,}|gh[pousr]_[A-Za-z0-9]{20,}|eyJ[A-Za-z0-9_-]{10,}\.eyJ' < <(head -c 200000 "$f"); then return 1; fi
  return 0
}

input="$(cat)"
command -v jq >/dev/null 2>&1 || exit 0 # no jq: do not block work
IFS=$'\t' read -r file offset limit < <(jq -r '.tool_input | [.file_path // "", .offset // "", .limit // ""] | @tsv' <<<"$input")

[ -n "$file" ] && [ -f "$file" ] || exit 0
case "$file" in *.png|*.jpg|*.jpeg|*.gif|*.webp|*.pdf|*.ipynb) exit 0 ;; esac

case "$file" in
  */web/dist/* | */package-lock.json | *.sql.go | */internal/store/sqlc/* | */web/src/contract/generated.ts | */internal/api/openapi.json | */docs/schema.md)
    echo "read-guard: $file is generated or a lockfile. Read its source (migrations, internal/contract, queries/*.sql) instead." >&2
    exit 2 ;;
esac

[ -z "$offset" ] && [ -z "$limit" ] || exit 0

lines="$(wc -l <"$file" | tr -d ' ')"
if [ "$lines" -gt "$MAX_LINES" ]; then
  echo "read-guard: $file has $lines lines (limit $MAX_LINES for a whole-file read)." >&2
  if [ -n "${METERWISE_READER_URL:-}" ] && reader_ok "$file"; then
    body="$(jq -n --arg model "${METERWISE_READER_MODEL:-local}" --rawfile f <(head -c 200000 "$file") \
      '{model:$model, messages:[{role:"system",content:"Summarise this file in at most 400 words. Cite function and type names with their line numbers. No code blocks."},{role:"user",content:$f}]}')"
    if summary="$(curl -sS --max-time 60 -H 'content-type: application/json' -d "$body" "${METERWISE_READER_URL%/}/v1/chat/completions" | jq -r '.choices[0].message.content // empty')" && [ -n "$summary" ]; then
      echo "Summary from the local reader model (not the file itself):" >&2
      echo "$summary" >&2
    fi
  fi
  echo "Use Grep to find the part you need, then Read with offset/limit." >&2
  exit 2
fi
exit 0
