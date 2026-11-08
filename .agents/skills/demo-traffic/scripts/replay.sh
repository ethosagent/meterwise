#!/usr/bin/env bash
# replay.sh — POST every testdata/demo/*.json request to the workspace URL in
# METERWISE_DEMO_URL. File name prefix picks the provider route:
# openai-*.json -> /openai/v1/chat/completions, anthropic-*.json -> /anthropic/v1/messages.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"
url="${METERWISE_DEMO_URL:-}"
[ -n "$url" ] || { echo "✗ set METERWISE_DEMO_URL to the workspace proxy URL (http://127.0.0.1:8080/w/<id>)" >&2; exit 2; }
case "$url" in http://127.0.0.1:* | http://localhost:*) ;; *) echo "✗ demo traffic only goes to a loopback URL" >&2; exit 2 ;; esac
shopt -s nullglob
files=(testdata/demo/*.json)
[ "${#files[@]}" -gt 0 ] || { echo "✗ no fixtures in testdata/demo/" >&2; exit 2; }
sent=0
for f in "${files[@]}"; do
  case "$(basename "$f")" in
    openai-*) route=/openai/v1/chat/completions ;;
    anthropic-*) route=/anthropic/v1/messages ;;
    *) echo "skip $f (name must start with openai- or anthropic-)"; continue ;;
  esac
  code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "$url$route" -H 'content-type: application/json' -H 'authorization: Bearer demo-not-a-real-key' --data-binary "@$f")"
  echo "$code $f"
  sent=$((sent + 1))
done
echo "replayed $sent request(s)"
