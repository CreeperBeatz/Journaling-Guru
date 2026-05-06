#!/usr/bin/env bash
# Boots the JournAI dev stack:
#   1. brings up postgres + mailhog via docker compose
#   2. waits for postgres healthcheck
#   3. applies migrations
#   4. multiplexes api + worker + web through overmind (or concurrently)
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "${ROOT_DIR}"

if [[ ! -f .env ]]; then
  echo "[start] .env not found — copy .env.example to .env and fill secrets."
  exit 1
fi

# Export .env so child processes (overmind / pnpm / go) inherit it.
set -o allexport
# shellcheck disable=SC1091
source .env
set +o allexport

echo "[start] bringing up postgres + mailhog…"
docker compose up -d postgres mailhog

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

if command -v overmind >/dev/null 2>&1; then
  echo "[start] launching overmind (Procfile)…"
  exec overmind start -f Procfile
elif command -v concurrently >/dev/null 2>&1 || npx --no -- concurrently --version >/dev/null 2>&1; then
  echo "[start] overmind not found — falling back to npx concurrently."
  exec npx --no -- concurrently \
    --names api,worker,web \
    --prefix-colors blue,magenta,green \
    "cd backend && air" \
    "cd backend && air -c .air.worker.toml" \
    "cd frontend && pnpm dev"
else
  echo "[start] need 'overmind' or 'concurrently' on PATH. See README." >&2
  exit 1
fi
