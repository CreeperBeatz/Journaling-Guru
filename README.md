# Journaling Guru

Daily journaling app — written or spoken — with multi-period AI reflections, push reminders, and magic-link auth. Self-hosted SaaS.

> Architecture, build phases, and design decisions live in [`PLAN.md`](./PLAN.md). This README is just enough to get the dev stack running.

**Status: Phase 1 (skeleton) — `/healthz`, `/readyz`, `/api/version`, and a minimal React shell wired through Vite + TanStack Query. Auth, journal, voice, summaries, and push reminders land in subsequent phases.**

## Prerequisites

Development is bash-native on Ubuntu (or WSL2). You'll need:

| Tool          | Version       | Purpose                                        |
| ------------- | ------------- | ---------------------------------------------- |
| Go            | 1.22+         | Backend                                        |
| Node          | 20 or 22      | Frontend toolchain                             |
| pnpm          | 9+            | Frontend package manager                       |
| Docker + Compose v2 | latest  | Postgres for dev                               |
| `goose` CLI   | optional      | Hand-running migrations; `make migrate-*` wraps |
| `air`         | latest        | Go hot reload (`go install github.com/air-verse/air@latest`) |
| `overmind`    | optional      | Process multiplexer; falls back to `npx concurrently` |

Quick install (Ubuntu):

```bash
# Go
curl -L https://go.dev/dl/go1.22.5.linux-amd64.tar.gz | sudo tar -C /usr/local -xz
echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> ~/.bashrc

# Node + pnpm
curl -fsSL https://deb.nodesource.com/setup_20.x | sudo -E bash -
sudo apt-get install -y nodejs
corepack enable && corepack prepare pnpm@latest --activate

# Air (Go hot reload)
go install github.com/air-verse/air@latest

# Overmind (Procfile multiplexer; alternative: `npm i -g concurrently`)
sudo apt-get install -y tmux
go install github.com/DarthSim/overmind/v2@latest
```

## First run

```bash
cp .env.example .env       # fill SMTP_* (external provider) and any keys you have
cd frontend && pnpm install && cd ..
cd backend && go mod tidy && cd ..
./start-dev.sh
```

`start-dev.sh` brings up Postgres, applies migrations, and multiplexes the
dev processes (with hot reload via `air` and Vite HMR):

- **api**    — Go HTTP server on `:8080` (rebuilds on `.go` changes)
- **worker** — River-backed job runner (Phase 1 stub; idles until Phase 4)
- **web**    — Vite dev server on `:5173` (proxies `/api/*` to the backend)

Magic-link emails go through whatever `SMTP_*` you configure in `.env`
(external provider — Postmark, Resend, your own server, etc.).

Production tooling (`docker-compose.prod.yml`, Caddy, backups) lands in Phase 7.

## Verifying Phase 1

```bash
curl -i http://localhost:8080/healthz       # always 200 once api is up
curl -i http://localhost:8080/readyz        # 200 only when Postgres is reachable
curl    http://localhost:8080/api/version   # {"version":"v2-dev","phase":1}
```

Open <http://localhost:5173/health> — the page should display the JSON returned by `/api/version`. If the page reports "Backend unreachable", the Vite proxy didn't reach Go; check that `air` is running.

Database:

```bash
make psql                       # interactive psql against the dev DB
make migrate-status             # list applied migrations
```

## Layout

```
backend/
  cmd/api          # chi HTTP server (entry: cmd/api/main.go)
  cmd/worker       # River job runner (Phase 1 stub)
  cmd/migrate      # goose migration runner (uses go:embed of *.sql)
  internal/
    config         # env-driven Config; loaded by every binary
    httpapi        # router + middleware + handlers
    store          # pgx pool + migrations/
frontend/
  src/
    api            # fetch wrapper (credentials: 'include', X-Requested-With)
    components/ui  # shadcn primitives (button, card, …)
    features/      # feature folders — auth, journal, voice, summaries, push
    lib            # queryClient, cn helper
    sw             # custom service worker (push handler in Phase 5)
    styles         # Tailwind base + shadcn HSL tokens
```

## Production

`docker-compose.prod.yml` and a Caddyfile land in **Phase 7** along with backups, Sentry, and full account deletion. Don't deploy from `main` until that phase ships.

## License

TBD.
