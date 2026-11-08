#!/usr/bin/env bash
# PreToolUse(Read): block whole-file reads of large or generated text files so one
# Read does not eat the context budget. Reads with an offset or limit pass, except
# for generated/lock files, which are never worth reading (use the source instead).
# Exit 2 blocks the call and shows stderr to the agent.
#
# Optional reader mode: when METERWISE_READER_URL is set (an OpenAI-compatible
# server, for example a local model), a blocked large-file read is summarised by
# that model and the summary is returned in the refusal. Default off. Secret-like
# files are never sent. METERWISE_READER_MODEL names the model (default "local").
set -euo pipefail
MAX_LINES="${READ_GUARD_MAX_LINES:-400}"

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
  if [ -n "${METERWISE_READER_URL:-}" ]; then
    case "$file" in
      *.env | */.env* | *.pem | *.key | *secret* | *credential* | *token*) ;;
      *)
        body="$(jq -n --arg model "${METERWISE_READER_MODEL:-local}" --rawfile f <(head -c 200000 "$file") \
          '{model:$model, messages:[{role:"system",content:"Summarise this file in at most 400 words. Cite function and type names with their line numbers. No code blocks."},{role:"user",content:$f}]}')"
        if summary="$(curl -sS --max-time 60 -H 'content-type: application/json' -d "$body" "${METERWISE_READER_URL%/}/v1/chat/completions" | jq -r '.choices[0].message.content // empty')" && [ -n "$summary" ]; then
          echo "Summary from the local reader model (not the file itself):" >&2
          echo "$summary" >&2
        fi ;;
    esac
  fi
  echo "Use Grep to find the part you need, then Read with offset/limit." >&2
  exit 2
fi
exit 0
