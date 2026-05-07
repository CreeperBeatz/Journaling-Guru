# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Source of truth

- **`PLAN.md`** at the repo root is authoritative for architecture, build phases, schema, and decisions. Read it before designing anything new. Phases (1=skeleton, 2=auth, 3=journal, 4=summaries, 4.1=manual mood/emotions, 5=push, 6a=chat, 6b=voice, 7=prod hardening) are sequential — don't claim a phase done without running its verification block.
- **`frontend/DESIGN.md`** is authoritative for the visual + motion system (warm-paper aesthetic, ink-violet primary, palette tokens, type stack, motion library). Update it when design changes; do not let code drift from it.

## Stack

- Backend: Go 1.25, `chi` router, `pgx/v5` (no GORM), `goose` migrations (go:embed), `riverqueue/river` v0.35 (Postgres-backed jobs), `caarlos0/env/v10` config, `log/slog`. Three binaries under `backend/cmd/`: `api`, `worker`, `migrate`, plus `vapid` utility.
- Frontend: Vite + React 18 + TS, TanStack Query, React Router v6 (lazy routes with shape-aware Suspense skeletons), Tailwind 3 (HSL CSS-var tokens), shadcn-style primitives + Radix, `motion` 12.x, `next-themes`, `sonner`, `vite-plugin-pwa` in **`injectManifest` mode** (`src/sw/push-handler.ts` is the real SW).
- Postgres only — no Redis. Caddy in prod terminates TLS and serves SPA + reverse-proxies `/api/*` (same-origin → no CORS in prod).

## Common commands

Dev workflow runs through `make` and `./start.sh` (bash, Ubuntu/WSL2 only):

```bash
make dev                 # ./start.sh — postgres up, migrations, then overmind multiplexes api+worker+web
./start.sh --tunnel      # cloudflared quick tunnel (HTTPS, ephemeral URL — for PWA install / push testing)
./start.sh --ngrok       # ngrok with reserved domain (stable URL across runs; set NGROK_URL in .env)
./start.sh --localhost   # bind only to 127.0.0.1
make psql                # interactive psql against dev DB
make migrate-up          # apply pending migrations (also runs River's own migrations)
make migrate-status      # show migration status
make vapid               # print fresh VAPID keypair to paste into .env
make fmt vet tidy        # backend hygiene
make build-api build-worker build-migrate
```

Frontend lint/build (no eslint — TS is the lint):

```bash
cd frontend && pnpm lint     # tsc --noEmit
cd frontend && pnpm build    # tsc -b && vite build
cd frontend && pnpm dev      # vite — usually launched by start.sh, not directly
```

Backend has no test suite yet. Verify by `go build ./...` + `go vet ./...` and the per-phase manual verification in PLAN.md.

`overmind` socket lives at `/tmp/overmind-journai.sock` (DrvFs at `/mnt/c` doesn't support AF_UNIX bind). Use `overmind connect` / `overmind kill` with `OVERMIND_SOCKET` set to that path.

## Architecture (the parts that need cross-file reading)

### Two binaries, one DB, dispatcher-tick model

`cmd/api` serves HTTP. `cmd/worker` runs River + a 60s **dispatcher tick** (`SUMMARY_DISPATCH_INTERVAL_SECONDS`) that drains four cooperating queues with `FOR UPDATE SKIP LOCKED`: `summary_jobs`, `emotion_classify_jobs`, `reminder_jobs`, `chat_extraction_jobs`. Each tick atomically claims due rows and inserts a corresponding River job (`SummaryArgs` / `EmotionClassifyArgs` / `ReminderArgs` / `ChatExtractionArgs`). The chat idle sweeper runs in the same tick and writes new `chat_extraction_jobs` rows for stale sessions, which the next claim block picks up.

This separation matters: the `*_jobs` tables are the **single source of truth** for scheduling; River is just the executor. Replicas can scale independently.

### Multi-tenancy = `WHERE user_id = $1` in store layer only

Every domain row carries `user_id`. **All scoped reads/writes go through `internal/store/*.go`** — handlers must never run raw `db.Query` (CI grep enforces this). RLS is intentionally not used; application-level scoping is faster to debug.

### Idempotency anchors live in unique indexes

The schema is built around composite uniqueness so retries can use `ON CONFLICT DO NOTHING`:

- `journal_entries (user_id, question_id, local_date)`
- `daily_inputs (user_id, local_date)`
- `summaries (user_id, period_type, period_start)`
- `summary_jobs (user_id, period_type, period_start)`, `reminder_jobs (user_id, fire_at)`
- `chat_sessions (user_id, local_date)`
- `questions (user_id, position)` is `DEFERRABLE INITIALLY DEFERRED` so reorder transactions work.

Worker logic is "if row exists for (anchor) skip; else compose, call LLM, INSERT ... ON CONFLICT DO NOTHING, mark job done." Don't break this.

### `local_date` is computed server-side from user TZ + `day_start_minutes`

`internal/timezone/local_date.go` is the single source for "what day is now in this user's life." Every write that maps to "today" uses it. `users.day_start_minutes` (default 360 = 6am) is a late-night cutoff: a 1am reflection still files under yesterday. **Don't** feed an already-canonical DB date back through `LocalDate` — that re-applies the offset and silently shifts dates by a day. `internal/timezone/period.go` distinguishes `PeriodContaining` (uses `LocalDate` normalization for `time.Now()` inputs) from `PeriodFromLocalStart` (skips it for stored period_starts).

### Magic-link auth (security-critical — see `internal/auth/`)

- `POST /api/auth/magic-link` always returns 200 (no enumeration). Stores `sha256(token)` only; raw token never persisted.
- `POST /api/auth/verify` does atomic `UPDATE ... SET consumed_at = now() WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now() RETURNING user_id`. Zero rows = reject. Single-use is enforced by this UPDATE — preserve it.
- Sessions: cookie holds raw 32 random bytes; DB stores `sha256`. `SameSite=Lax` (Strict would break the email click).
- CSRF: `SameSite=Lax` + custom-header check (`X-Requested-With: fetch`) on mutating endpoints, applied via `mw.CSRF` in router.

### Manual-wins merge (Phase 4.1 / 6a)

User-entered mood/emotions/notes (`daily_inputs`) and manually written entries (`journal_entries`) are the source of truth. The chat extraction worker uses **manual-wins** semantics:

- `DailyInputStore.MergeFromExtraction`: COALESCE for mood, CASE-WHEN-empty for text.
- `EntryStore.UpsertIfAbsent`: `ON CONFLICT DO NOTHING`.

So a manual edit *during* the extraction window is never clobbered. The daily LLM prompt only generates `{body, topics}` — it does **not** infer mood/emotions; those come from `daily_inputs`.

### Chat SSE (Phase 6a)

`/api/chat/*` mounts under chi **without `chimw.Timeout`** — the Timeout middleware's wrapper does not implement `http.Flusher`, which kills SSE streaming. The other `/api/*` group keeps a 30s Timeout; chat relies on client-disconnect (request context) for cancellation. Don't reintroduce a global Timeout.

SSE frame schema: `token | tool | phase | crisis | error | done`. Crisis path is **server-side regex** (`internal/llm/chat/safety.go`) — when triggered, persist user msg + `system_event`, emit one `crisis` frame, and **do not call the LLM**.

OpenRouter client gained per-call `Model` override + `CompleteStream` + `SystemCacheable` (Anthropic prompt-cache marker, no-op for other providers). Three model tiers live in env: `CHAT_MODEL` (streaming chat turns), `SUMMARY_MODEL` (async summaries), `CLASSIFY_MODEL` (emotion classify, chat coverage, post-session extraction). Each tier gets its own `OpenRouter` client constructed in `cmd/worker` and `httpapi/router`; per-call `Model` override on the request still wins (used to honor session pins on `chat_sessions.chat_model` / `extraction_model`).

### Push (Phase 5) — iOS gate is real

- `webpush-go` v1.4. `Sender.Outcome` is `Delivered | Gone | Retryable`. `410 Gone` → delete the subscription row. After 5 failures → also delete.
- `pushsubscriptionchange` SW handler is the iOS reliability linchpin — it re-fetches the VAPID key and re-subscribes. Don't remove it.
- iOS Web Push only works **after Add to Home Screen** on iOS 16.4+. The frontend's `RemindersCard` has a dedicated branch for this; preserve it.
- `nextReminderFireAt` deliberately ignores `day_start_minutes` — reminder is keyed off raw clock time, not user-day semantics.

### Voice (Phase 6b — schema-ready, not implemented)

The chat tables (`chat_sessions.mode='voice'`, `openai_session_id`) are voice-ready as of Phase 6a. Voice will mint an OpenAI Realtime ephemeral `client_secret` server-side; **the browser connects to OpenAI directly via WebRTC** — audio never touches the Go backend. Don't build a WebSocket audio proxy.

## Frontend conventions

- Routes are lazy-loaded; each has a shape-aware Suspense skeleton in the same feature folder. `AppShell` (in `components/shell/`) gates the `/api/me` redirect — don't add a per-route AuthGate.
- `AppShell` prefetches keys on mount (questions, today entries, daily inputs, today chat session, summaries stats) — when adding a feature on the protected shell, consider adding its query key to the prefetch fan-out.
- Service worker is custom (`src/sw/push-handler.ts`) — `vite-plugin-pwa` injects the precache manifest into it. Workbox runtime cache is `NetworkOnly` for `/api/*`.
- The SPA is a single origin via the Vite proxy in dev (`/api` → `localhost:8080`). `VITE_API_BASE` should stay empty in plain dev — session cookies are `SameSite=Lax` and won't cross origins. `start.sh --tunnel` / `--ngrok` sets `VITE_API_BASE` to the public URL **and** `COOKIE_SECURE=true`. The Vite proxy target is `localhost:8080` regardless, to avoid a tunnel loop.
- Palette + theme: `<html class="dark" data-palette="...">`. An anti-flash script in `index.html` sets `data-palette` from `localStorage["journai.palette"]` before React hydrates. Theme is `next-themes` with `attribute="class"`, `disableTransitionOnChange`. Do not add unconditional theme-flip transitions.

## Conventions and gotchas

- **Don't bypass the store layer.** Add a method to `internal/store/<resource>.go` rather than running `db.Query` from a handler or worker.
- **Don't add a global request Timeout to `/api/chat`** — the response writer must support `Flusher` for SSE.
- **Lazy-seed scheduling** is owned by the api binary (entry write triggers `Scheduler` / `ReminderScheduler`); `ScheduleNext` after a job fires is owned by the worker.
- **Inactivity dormancy** (`SUMMARY_INACTIVITY_DAYS`, default 30) pauses re-arming day/week/month summaries for users who stopped journaling. Yearly always re-arms. Lazy-seed re-engages on the next entry write.
- **Kill stale `overmind`** before starting a fresh worker. A previous bug had two workers running concurrently and writing inconsistent metadata. `overmind kill -s /tmp/overmind-journai.sock` if needed.
- **Migrations are embedded** via `backend/internal/store/migrations/embed.go`. `cmd/migrate up` also runs `rivermigrate.Migrate(Up)` for River's tables. New migration: add a numbered SQL file in that dir; `goose` picks it up.
- **Commit identity**: commits should be authored as `Dani Matev <dani.matev123@gmail.com>` (handle `CreeperBeatz`), NOT `dani@cosmosthrace`. (See user memory.)

## Things deliberately NOT in v1 of v2

Redis, Postgres RLS, Vault/SOPS, React Native/Capacitor, per-user LLM model selection, Stripe billing, Prometheus/Grafana, migration from v1 (Streamlit/docarray — v1 is stale, v2 is empty start). Don't propose any of these without explicit user direction.
