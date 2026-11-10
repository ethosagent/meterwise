#!/usr/bin/env bash
# Native dev runner behind `make dev` / `make dev-stop`: bin/meterwised serve + the Vite dev server in the
# background. State lives in .dev/ (pids, logs). The native serve runs APP_ENV=dev with no CLERK_* set, so the
# control plane signs every request in as the seeded dev admin (see docs/runbooks/local-dev.md).
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/../.."
D=.dev; mkdir -p "$D"
API_PORT=${APP_API_PORT:-8081}; PROXY_PORT=${APP_PROXY_PORT:-8080}; WEB_PORT=${WEB_PORT:-5173}

alive() { [ -f "$D/$1.pid" ] && kill -0 "$(cat "$D/$1.pid")" 2>/dev/null; }
stop_one() {
  if alive "$1"; then kill "$(cat "$D/$1.pid")" 2>/dev/null || true; echo "stopped $1"; fi
  rm -f "$D/$1.pid"
}
wait_http() { # wait_http <url> <name>
  for _ in $(seq 1 60); do curl -fs "$1" >/dev/null 2>&1 && return 0; sleep 0.5; done
  echo "$2 did not come up; see $D/$2.log" >&2; tail -n 15 "$D/$2.log" >&2 || true; return 1
}

case "${1:-}" in
start)
  : "${DATABASE_URL:?DATABASE_URL is not set: run make setup (creates .env) and make infra-up}"
  [ -x bin/meterwised ] || { echo "bin/meterwised missing: run make build" >&2; exit 1; }
  [ -x web/node_modules/.bin/vite ] || (cd web && npm ci --no-audit --no-fund)
  alive meterwised && { echo "already running: make dev-stop first" >&2; exit 1; }
  trap 'stop_one web; stop_one meterwised' ERR
  echo "==> migrate"; bin/meterwised migrate
  echo "==> meterwised serve (APP_ENV=dev)"
  APP_ENV=dev LISTEN_API="127.0.0.1:$API_PORT" LISTEN_PROXY="127.0.0.1:$PROXY_PORT" LISTEN_ADMIN="127.0.0.1:${APP_ADMIN_PORT:-9090}" APP_URL="http://127.0.0.1:$WEB_PORT" \
    nohup bin/meterwised serve >"$D/meterwised.log" 2>&1 </dev/null & echo $! >"$D/meterwised.pid"
  wait_http "http://127.0.0.1:$API_PORT/healthz" meterwised || { stop_one meterwised; exit 1; }
  echo "==> web dev server"
  METERWISE_API_URL="http://127.0.0.1:$API_PORT" nohup web/node_modules/.bin/vite web --host 127.0.0.1 --port "$WEB_PORT" --strictPort \
    >"$D/web.log" 2>&1 </dev/null & echo $! >"$D/web.pid"
  wait_http "http://127.0.0.1:$WEB_PORT/" web || { stop_one web; stop_one meterwised; exit 1; }
  cat <<MSG

  Web app (hot reload) http://127.0.0.1:$WEB_PORT      signed in as the seeded dev admin
  API + built web      http://127.0.0.1:$API_PORT/healthz
  Proxy                http://127.0.0.1:$PROXY_PORT    (workspace URLs: copy from Settings > Workspaces)
  Logs                 $D/meterwised.log  $D/web.log   (make dev-logs)
  Stop                 make dev-stop
MSG
  ;;
stop) stop_one web; stop_one meterwised ;;
logs) exec tail -n 50 -f "$D/meterwised.log" "$D/web.log" ;;
*) echo "usage: native.sh start|stop|logs" >&2; exit 2 ;;
esac
