#!/usr/bin/env bash
# Boots the JournAI dev stack with hot reload (air + Vite HMR):
#   1. brings up postgres via docker compose
#   2. waits for postgres healthcheck
#   3. applies migrations
#   4. multiplexes api + worker + web through overmind (or concurrently)
#
# By default Vite binds 0.0.0.0 so phones on the same Wi-Fi can reach it at
# http://<windows-ip>:5173. The script tries to detect that IP for you via
# powershell.exe. Over plain HTTP the PWA service worker / push won't register
# — use --tunnel or --ngrok for that.
#
# Flags:
#   --tunnel     start a cloudflared quick tunnel and inject PUBLIC_BASE_URL +
#                COOKIE_SECURE=true so magic links and Secure cookies work over
#                the public HTTPS URL. URL is ephemeral (random per run).
#   --ngrok      same as --tunnel but uses ngrok with a reserved domain
#                (default: champion-square-yak.ngrok-free.app, override via
#                NGROK_URL in .env). URL is stable across runs.
#   --localhost  bind only to localhost (no LAN exposure, no tunnel)
#
# SMTP is expected to be an external provider configured via .env.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT_DIR}"

MODE="lan"
for arg in "$@"; do
  case "$arg" in
    --tunnel) MODE="tunnel" ;;
    --ngrok) MODE="ngrok" ;;
    --localhost) MODE="localhost" ;;
    -h|--help)
      sed -n '2,22p' "${BASH_SOURCE[0]}"
      exit 0
      ;;
    *) echo "[start] unknown arg: $arg" >&2; exit 1 ;;
  esac
done

# Best-effort: ask Windows for the LAN-side IPv4 of a real adapter (Wi-Fi /
# Ethernet), skipping WSL/Hyper-V virtual switches and link-local addresses.
detect_windows_lan_ip() {
  command -v powershell.exe >/dev/null 2>&1 || return 1
  powershell.exe -NoProfile -Command \
    "Get-NetIPAddress -AddressFamily IPv4 | Where-Object { \$_.PrefixOrigin -in 'Dhcp','Manual' -and \$_.InterfaceAlias -notmatch 'WSL|vEthernet|Loopback|Hyper-V' -and \$_.IPAddress -notmatch '^(127\.|169\.254\.)' } | Select-Object -First 1 -ExpandProperty IPAddress" \
    2>/dev/null | tr -d ' \r\n'
}

if [[ ! -f .env ]]; then
  echo "[start] .env not found — copy .env.example to .env and fill secrets."
  exit 1
fi

# Export .env so child processes (overmind / pnpm / go) inherit it.
set -o allexport
# shellcheck disable=SC1091
source .env
set +o allexport

echo "[start] bringing up postgres…"
docker compose up -d postgres

echo "[start] waiting for postgres…"
for i in {1..60}; do
  if docker compose exec -T postgres pg_isready -U journai -d journai >/dev/null 2>&1; then
    break
  fi
  if [[ $i -eq 60 ]]; then
    echo "[start] postgres did not become ready in 60s" >&2
    exit 1
  fi
  sleep 1
done

echo "[start] applying migrations…"
( cd backend && go run ./cmd/migrate up )

TUNNEL_PID=""
TUNNEL_LOG=""
cleanup() {
  if [[ -n "${TUNNEL_PID}" ]] && kill -0 "$TUNNEL_PID" 2>/dev/null; then
    kill "$TUNNEL_PID" 2>/dev/null || true
  fi
  if [[ -n "${TUNNEL_LOG}" && -f "${TUNNEL_LOG}" ]]; then
    rm -f "$TUNNEL_LOG"
  fi
}

if [[ "$MODE" == "lan" ]]; then
  export VITE_DEV_HOST=true
  WIN_IP="$(detect_windows_lan_ip || true)"
  if [[ -n "${WIN_IP}" ]]; then
    echo "[start] Vite will bind 0.0.0.0 — open http://${WIN_IP}:5173 on a device on the same Wi-Fi."
  else
    echo "[start] Vite will bind 0.0.0.0 — couldn't auto-detect the Windows LAN IP; run 'ipconfig' on Windows and open http://<that-ip>:5173."
  fi
  echo "[start] (PWA install / push need HTTPS — use './start.sh --tunnel' for that.)"
fi

if [[ "$MODE" == "localhost" ]]; then
  echo "[start] localhost-only mode: Vite stays on 127.0.0.1."
fi

if [[ "$MODE" == "tunnel" ]]; then
  if ! command -v cloudflared >/dev/null 2>&1; then
    echo "[start] cloudflared not on PATH. Install via: https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/" >&2
    exit 1
  fi

  TUNNEL_LOG="$(mktemp -t cloudflared.XXXXXX.log)"
  trap cleanup EXIT INT TERM

  echo "[start] starting cloudflared quick tunnel → localhost:5173…"
  cloudflared tunnel --url http://localhost:5173 >"$TUNNEL_LOG" 2>&1 &
  TUNNEL_PID=$!

  TUNNEL_URL=""
  for i in {1..30}; do
    if ! kill -0 "$TUNNEL_PID" 2>/dev/null; then
      echo "[start] cloudflared exited unexpectedly. log:" >&2
      cat "$TUNNEL_LOG" >&2
      exit 1
    fi
    TUNNEL_URL=$(grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' "$TUNNEL_LOG" | head -1 || true)
    if [[ -n "$TUNNEL_URL" ]]; then
      break
    fi
    sleep 1
  done

  if [[ -z "$TUNNEL_URL" ]]; then
    echo "[start] timed out waiting for cloudflared URL. log:" >&2
    cat "$TUNNEL_LOG" >&2
    exit 1
  fi

  export PUBLIC_BASE_URL="$TUNNEL_URL"
  export COOKIE_SECURE=true
  # SPA calls the API through the same public URL; Vite proxies /api to the
  # local backend (proxy target stays localhost:8080 to avoid a tunnel loop).
  export VITE_API_BASE="$TUNNEL_URL"
  # Vite needs to accept the tunnel domain in the Host header.
  export VITE_DEV_HOST=true
  echo "[start] tunnel ready → $TUNNEL_URL  (open this on your phone)"
fi

if [[ "$MODE" == "ngrok" ]]; then
  if ! command -v ngrok >/dev/null 2>&1; then
    echo "[start] ngrok not on PATH. Install via: https://ngrok.com/download" >&2
    exit 1
  fi

  NGROK_HOST="${NGROK_URL:-champion-square-yak.ngrok-free.app}"
  # Strip any scheme the user may have added to NGROK_URL.
  NGROK_HOST="${NGROK_HOST#https://}"
  NGROK_HOST="${NGROK_HOST#http://}"

  TUNNEL_LOG="$(mktemp -t ngrok.XXXXXX.log)"
  trap cleanup EXIT INT TERM

  echo "[start] starting ngrok → https://${NGROK_HOST} → localhost:5173…"
  ngrok http "5173" --url="${NGROK_HOST}" --log=stdout --log-format=logfmt >"$TUNNEL_LOG" 2>&1 &
  TUNNEL_PID=$!

  TUNNEL_READY=""
  for i in {1..30}; do
    if ! kill -0 "$TUNNEL_PID" 2>/dev/null; then
      echo "[start] ngrok exited unexpectedly. log:" >&2
      cat "$TUNNEL_LOG" >&2
      exit 1
    fi
    if grep -qE 'started tunnel|url=https://' "$TUNNEL_LOG" 2>/dev/null; then
      TUNNEL_READY=1
      break
    fi
    sleep 1
  done

  if [[ -z "$TUNNEL_READY" ]]; then
    echo "[start] timed out waiting for ngrok tunnel. log:" >&2
    cat "$TUNNEL_LOG" >&2
    exit 1
  fi

  export PUBLIC_BASE_URL="https://${NGROK_HOST}"
  export COOKIE_SECURE=true
  # SPA calls the API through the same public URL; Vite proxies /api to the
  # local backend (proxy target stays localhost:8080 to avoid a tunnel loop).
  export VITE_API_BASE="${PUBLIC_BASE_URL}"
  # Vite needs to accept the tunnel domain in the Host header.
  export VITE_DEV_HOST=true
  echo "[start] ngrok ready → ${PUBLIC_BASE_URL}  (open this on your phone)"
fi

run_stack() {
  if command -v overmind >/dev/null 2>&1; then
    # /mnt/c (DrvFs) doesn't support AF_UNIX bind, so put overmind's control
    # socket on the Linux-native filesystem. Use the same OVERMIND_SOCKET when
    # running `overmind connect` / `overmind kill` against this session.
    export OVERMIND_SOCKET="${OVERMIND_SOCKET:-/tmp/overmind-journai.sock}"
    echo "[start] launching overmind (Procfile, socket=${OVERMIND_SOCKET})…"
    overmind start -f Procfile
  elif command -v concurrently >/dev/null 2>&1 || npx --no -- concurrently --version >/dev/null 2>&1; then
    echo "[start] overmind not found — falling back to npx concurrently."
    npx --no -- concurrently \
      --names api,worker,web \
      --prefix-colors blue,magenta,green \
      "cd backend && air" \
      "cd backend && air -c .air.worker.toml" \
      "cd frontend && pnpm dev --host 0.0.0.0"
  else
    echo "[start] need 'overmind' or 'concurrently' on PATH. See README." >&2
    exit 1
  fi
}

# When a tunnel is on we need the EXIT trap to run so the tunnel process is
# killed, so we can't `exec` the supervisor — keep it as a child and `wait`.
if [[ -n "$TUNNEL_PID" ]]; then
  run_stack &
  STACK_PID=$!
  wait "$STACK_PID"
else
  run_stack
fi
