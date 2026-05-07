# JournAI v2 — Implementation Plan

## Context

This is **v2** of [CreeperBeatz/JournAI](https://github.com/CreeperBeatz/JournAI). v1 was a Python/Streamlit prototype using docarray-on-SQLite and GPT-3.5 with weekly summaries. v2 is a full rewrite — different language, different framework, different DB layer, different auth model — keeping only the domain idea (daily questions + AI reflection summaries). v1 is stale and not migrated; v2 starts from an empty repo.

The differentiators v2 adds beyond what v1 had:

1. **Voice journaling** — "call your journal" via OpenAI Realtime; the conversation becomes the day's entry.
2. **Multi-period summaries** — daily, weekly, monthly, yearly (v1 was weekly only).
3. **Push reminders on mobile** — without writing native code (PWA + Web Push).
4. **Multi-tenant SaaS** — anyone can sign up; magic-link only auth.
5. **Vendor-agnostic LLM** — OpenRouter for summaries, so we're not locked to OpenAI for text.

Confirmed decisions:

- **Stack**: Go + Postgres + React (PWA). Monorepo with `backend/` and `frontend/`, `start.sh` at root.
- **Mobile**: PWA + Web Push (VAPID). No React Native, no Capacitor.
- **Auth**: Magic-link only (passwordless), HTTP-only cookie sessions.
- **Multi-tenant SaaS** — anyone can sign up; each user owns their data.
- **Voice**: OpenAI Realtime API, browser → OpenAI direct via WebRTC + ephemeral token minted by backend.
- **Summaries**: OpenRouter (default `anthropic/claude-sonnet-4-5`).
- **Question set**: ship sensible defaults, user can edit/add/reorder.
- **Dev environment**: Ubuntu (development is bash-native; no Windows path supported).
- **Deployment**: self-hosted VPS (Hetzner/DO), docker-compose + Caddy for HTTPS.

## Architecture at a glance

- `backend/cmd/api` — chi HTTP server.
- `backend/cmd/worker` — River-backed job runner (summaries + push dispatch). Scales independently in prod.
- `backend/cmd/migrate` — goose migrations.
- `frontend/` — Vite + React 18 + TS, PWA via `vite-plugin-pwa` (`injectManifest` mode so we hand-write the push handler).
- Postgres — sole stateful store. No Redis in v1.
- Caddy — TLS termination + reverse proxy `/api/*` → backend, static frontend everywhere else (same origin → no CORS).

### Library picks

- **Router**: `chi`.
- **DB**: `pgx/v5` + `sqlc` (no GORM — fights timezone-aware partial indexes).
- **Migrations**: `goose` with `go:embed`.
- **Job queue**: `riverqueue/river` (Postgres-backed, retries, periodic jobs, observability).
- **Web Push**: `SherClockHolmes/webpush-go`.
- **Validation**: `go-playground/validator/v10`.
- **Config**: `caarlos0/env/v10`.
- **Logging**: `log/slog` (JSON in prod, text in dev).
- **Frontend**: TanStack Query for server state, React Router v6, Tailwind + shadcn/ui, `react-markdown` + `remark-gfm`, `date-fns` + `date-fns-tz`.
- **SMTP**: Mailhog in dev, **Postmark** in prod (deliverability matters for magic links).

## Repo layout

```
JournAI/
  PLAN.md                        # this plan, copied into the repo so claude-code can pick it up
  start.sh                       # bash launcher (dev)
  docker-compose.yml             # dev: postgres + mailhog
  docker-compose.prod.yml        # prod: postgres + backend + worker + caddy
  Caddyfile
  Procfile                       # overmind processes for `start.sh`
  Makefile
  .env.example                   # template; real .env gitignored
  .gitignore

  backend/
    go.mod
    cmd/
      api/main.go
      worker/main.go
      migrate/main.go
    internal/
      config/                    # env loading
      httpapi/
        router.go                # chi wiring
        middleware/              # auth, rate-limit, request-id, recover, csrf
        handlers/                # auth, questions, entries, voice, summaries, push, health
      auth/                      # magic-link gen/verify, session lifecycle
      domain/                    # pure types
      store/
        queries/                 # sqlc .sql sources
        migrations/              # goose .sql files
        db.go                    # pgxpool + tx helpers
      mail/                      # interface + smtp impl
      llm/
        openrouter.go
        prompts/                 # go:embed templates for daily/weekly/monthly/yearly
      realtime/
        openai.go                # mints ephemeral client_secret
      push/
        webpush.go
        scheduler.go
      jobs/
        runner.go                # river registration
        daily_summary.go
        weekly_summary.go
        monthly_summary.go
        yearly_summary.go
        push_dispatch.go
      timezone/
        local_date.go            # user-tz -> local_date helper
    .air.toml

  frontend/
    package.json
    vite.config.ts               # vite-plugin-pwa configured here
    public/
      icons/                     # 192, 512, maskable
      manifest.webmanifest
    src/
      main.tsx
      App.tsx
      router.tsx
      api/                       # fetch wrapper (credentials: 'include') + per-resource modules
      components/ui/             # shadcn/ui primitives
      features/
        auth/                    # MagicLinkRequest, MagicLinkVerify
        journal/                 # DailyEntry, QuestionEditor
        voice/                   # CallJournal, useRealtimeSession
        summaries/               # SummaryList, SummaryDetail
        push/                    # usePushSubscription
        history/                 # HistoryView
      lib/
        queryClient.ts
        date.ts
      sw/
        push-handler.ts          # custom SW: push + notificationclick + pushsubscriptionchange
      styles/
```

## Postgres schema (v1 of v2 — the first migration)

`0001_init.sql` enables `citext` and `pgcrypto`, then creates:

- **users** — `id uuid PK`, `email citext UNIQUE`, `email_verified bool`, `display_name`, `timezone text` (IANA), `reminder_time time` (local), `reminder_enabled bool`, `created_at`, `deleted_at` (soft delete for GDPR).
- **magic_link_tokens** — `id`, `user_id` FK, `token_hash bytea` (sha256 of raw token; raw never stored), `expires_at` (15 min), `consumed_at` (single-use), `ip_address`, `user_agent`. Index on `(token_hash)` and `(user_id, created_at)` for rate-limit queries.
- **sessions** — `token_hash bytea UNIQUE` (cookie holds raw 32-byte random; DB stores sha256), `user_id`, `expires_at`, `last_seen_at`, `user_agent`, `ip_address`.
- **questions** — `id`, `user_id`, `prompt text`, `position int`, `archived_at` (soft delete preserves entry FKs), `UNIQUE (user_id, position) DEFERRABLE INITIALLY DEFERRED`.
- **journal_entries** — `id`, `user_id`, `question_id`, `local_date date` (computed server-side from user TZ at write time), `body text` (markdown), `source` ('text' | 'voice'), `voice_session_id` FK NULL, timestamps. **`UNIQUE (user_id, question_id, local_date)`** is the linchpin — one entry per question per day, "day" defined in user's TZ.
- **voice_sessions** — `id`, `user_id`, `local_date`, `openai_session_id`, `started_at`, `ended_at`, `duration_seconds`, `transcript_raw jsonb`, `status` ('pending' | 'active' | 'completed' | 'failed'), `cost_cents`.
- **summaries** — `id`, `user_id`, `period_type` ('day' | 'week' | 'month' | 'year'), `period_start date`, `period_end date`, `body text`, `model`, `prompt_tokens`, `completion_tokens`, `generated_at`. **`UNIQUE (user_id, period_type, period_start)`** — idempotency anchor for retries.
- **push_subscriptions** — `id`, `user_id`, `endpoint UNIQUE`, `p256dh`, `auth`, `user_agent`, `last_used_at`, `failed_count`. Delete row on `410 Gone`.
- **reminder_jobs** — `id`, `user_id`, `fire_at timestamptz` (absolute UTC), `fired_at`, `status`, `attempts`, `UNIQUE (user_id, fire_at)`. Lazy creation: enqueue tomorrow's row when today's fires.
- **summary_jobs** — same shape as reminder_jobs but with `period_type` + `period_start`. `UNIQUE (user_id, period_type, period_start)`.

**Multi-tenancy enforcement**: every domain row carries `user_id`; centralize `WHERE user_id = $1` in the `store` layer; CI grep rejects raw `db.Query` calls outside `store/`. Skip Postgres RLS in v1 — application-level scoping is faster to debug.

## Magic-link flow (security-critical)

1. `POST /auth/magic-link` with `{email}` → backend always returns 200 (no user enumeration). Generates 32 bytes from `crypto/rand`, base64-url-encodes → that's the link token. Stores `sha256(token)` in `magic_link_tokens`.
2. Email contains `https://app/auth/verify?token=<raw>`. **15-minute TTL.**
3. Rate-limit: 3 sends per email per 15 min, 10 per email per day, 20 per IP per hour. Counter via Postgres queries on `magic_link_tokens`.
4. `GET /auth/verify?token=...` → atomic `UPDATE magic_link_tokens SET consumed_at = now() WHERE token_hash = $1 AND consumed_at IS NULL AND expires_at > now() RETURNING user_id`. Zero rows = reject.
5. On success, mint a session: `Set-Cookie: session=<random32>; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=2592000`. Lax (not Strict) is required so the email click works.
6. CSRF: SameSite=Lax + custom-header check (`X-Requested-With: fetch`) on mutating endpoints.

## Voice — WebRTC + ephemeral tokens (NOT WebSocket proxy)

The browser connects to OpenAI **directly via WebRTC** using a short-lived token minted by the Go backend. Audio never touches your server.

1. Browser → `POST /voice/session` (cookie-authed).
2. Backend checks per-user monthly minute quota → calls `POST https://api.openai.com/v1/realtime/client_secrets` with system instructions composed from the user's questions ("Ask each of these conversationally..."), `input_audio_transcription` enabled. Inserts `voice_sessions` row (`status='pending'`). Returns `{client_secret, expires_at, voice_session_id}`.
3. Browser opens WebRTC peer connection to OpenAI using the ephemeral secret as bearer. Opens a data channel; subscribes to transcript events.
4. Browser also opens a thin WebSocket to `/voice/sessions/:id/events` and forwards transcript events for durability.
5. On call end, browser `POST /voice/sessions/:id/finalize` with full transcript.
6. Backend runs a **mapping LLM call** via OpenRouter (cheap model): "Here are the user's questions [...] and the transcript [...]. Return JSON `{question_id: answer_text}`." Writes one `journal_entries` row per question with `source='voice'`.

Why not proxy: WebRTC handles jitter/RTP/packet loss natively. Proxying through Go means re-implementing audio transport — guaranteed glitches.

## Summary jobs

- River registers four periodic jobs per user: daily, weekly, monthly, yearly.
- Fire times computed at job-creation time as absolute UTC from user-local 00:30 + `period_start`.
- Worker logic per job:
  ```
  IF row exists in summaries for (user, period_type, period_start) → skip (idempotent).
  ELSE compose prompt from go:embed template, call OpenRouter chat completions,
       INSERT ... ON CONFLICT DO NOTHING, mark job done.
  ```
- Token-budget control: weekly prompt includes 7 daily summaries (not raw entries); monthly includes weekly summaries; yearly includes monthly summaries.
- Default model: `anthropic/claude-sonnet-4-5` via OpenRouter. Per-user model preference is a v2.x feature.
- River retries with exponential backoff; after 5 failures mark `summary_jobs.status='failed'` and stop auto-enqueuing the next period.

## Web Push

- `webpush.GenerateVAPIDKeys()` once at setup. Public key in `.env` and exposed to frontend as `VITE_VAPID_PUBLIC_KEY`. Private key server-only. Subject: `mailto:dani@cosmosthrace.com`.
- On user save (timezone or reminder_time changed) compute next 7 days of `reminder_jobs`.
- Worker tick every 60s: `SELECT id FROM reminder_jobs WHERE status='pending' AND fire_at <= now() FOR UPDATE SKIP LOCKED LIMIT 100` → dispatch → mark sent → enqueue tomorrow's row.
- `410 Gone` from push service → delete subscription. `429`/`5xx` → backoff; after 5 failures delete.
- Service worker handles `push` (`showNotification`), `notificationclick` (`clients.openWindow('/today')`), and **`pushsubscriptionchange`** (re-subscribe and POST to backend — critical for iOS reliability).
- **iOS gate**: web push only works after "Add to Home Screen" on iOS 16.4+. Onboarding must surface this explicitly.

## Local dev (Ubuntu)

`start.sh` (bash, executable, on the developer's PATH-relative working dir):

1. `docker compose up -d postgres mailhog`.
2. Wait for Postgres healthcheck.
3. `go run ./backend/cmd/migrate up`.
4. Multiplex three processes via **overmind** reading `Procfile`:
   ```
   api:    cd backend && air
   worker: cd backend && air -c .air.worker.toml
   web:    cd frontend && pnpm dev
   ```
   One tmux session, single Ctrl-C teardown. Fall back to `concurrently` (npm) if overmind isn't installed; document in README.

Hot reload: `air` for Go, Vite HMR for frontend.

Tooling assumed on dev box: Go 1.22+, Node 20+ (or 22), pnpm, Docker + Compose v2, overmind (or concurrently), `goose` CLI.

## Secrets / env

- Dev: gitignored `.env` at repo root, loaded by backend (`caarlos0/env`) and Vite (`VITE_*`).
- Prod: `/etc/journai/.env` mounted read-only into containers.
- Keys split: `OPENAI_API_KEY` (Realtime only), `OPENROUTER_API_KEY` (summaries + transcript mapping), `VAPID_PUBLIC_KEY` / `VAPID_PRIVATE_KEY` / `VAPID_SUBJECT`, `SMTP_*`, `DATABASE_URL`.
- Configure SPF + DKIM + DMARC on the sender domain before launch — magic-link deliverability is the single biggest UX failure mode.

## Build phases

1. **Skeleton (1–2 days)** — repo scaffold, docker-compose, migration #1 (extensions + users/sessions/magic_link_tokens), `/healthz`, `/readyz`, Vite + React skeleton with Tailwind/shadcn/TanStack Query, `start.sh`.
2. **Auth (2–3 days)** — magic-link send/verify, cookie session middleware, frontend request + verify pages, rate limiting, account-deletion endpoint stubbed.
3. **Journal MVP (3–4 days)** — questions CRUD + reorder + seed defaults on signup, journal_entries with `local_date`, DailyEntry page, HistoryView.
4. **Summaries (3–4 days)** — `summaries` + `summary_jobs`, River worker, OpenRouter client, four prompt templates (daily first), SummaryList + SummaryDetail.
5. **Push reminders (3 days)** — VAPID setup, push_subscriptions + reminder_jobs, frontend subscribe flow with iOS install instructions, service worker push handler, dispatcher tick.
6. **Phase 6a — Chat (5–7 days)** — chat-first reflective journaling on /today. `chat_sessions`/`chat_messages`/`chat_extraction_jobs` migration, OpenRouter SSE streaming, `internal/llm/chat` package (system + extraction prompts, safety regex, tool defs), `ChatExtractionWorker` + idle sweeper, 7 HTTP endpoints under `/api/chat`, frontend `features/chat/` with streaming state machine + bubble UI + coverage chips + crisis card. DailyEntry becomes a Tabs dispatcher (Manual / Chat / Talk). Default mode resolution: `?mode=` URL → localStorage → `chat`. Idle auto-finalize at 20 min; explicit "I'm done" finalize anytime. Single-shot extraction (Haiku/Gemma-class) writes daily_inputs (manual-wins via `MergeFromExtraction`) + journal_entries with `source='chat'` (manual-wins via `UpsertIfAbsent`). Read-only transcript card slots into HistoryView.
   **Phase 6b — Voice (3–5 days)** — adds the OpenAI Realtime IO layer on top of 6a's tables. `POST /api/chat/sessions/:id/voice/start` mints an ephemeral OpenAI client_secret; the FE opens a WebRTC peer + data channel and forwards transcript deltas as `chat_messages` rows. `mode='voice'` flips on the existing session row; `openai_session_id` is populated. **No new migrations** — schema is voice-ready in 6a. Reuses `BuildSystemPrompt`, `ChatExtractionWorker`, all stores.
7. **Production hardening (3–4 days)** — `docker-compose.prod.yml` + Caddyfile, Postmark wired, `pg_dump` cron with off-host backup, Sentry on frontend, full account deletion, load test the worker tick.

## Verification

- **Skeleton**: `curl localhost:8080/healthz` → 200; `curl localhost:8080/readyz` → 200 only when Postgres is up; `pnpm dev` serves frontend.
- **Auth**: submit email → mailhog shows email → click link → cookie set → `/api/me` returns user. Replay link → 401. 4th magic-link in 15 min → 429.
- **Journal**: create/reorder/archive questions; save entry; edit entry; raw-SQL duplicate insert rejected; TZ change moves "today" correctly.
- **Summaries**: trigger daily job → row created. Trigger again → ON CONFLICT no-ops, no LLM call (verify in logs). Kill worker mid-call, restart → retry succeeds without duplicate.
- **Push**: subscribe on Chrome desktop → notification at fire_at. Subscribe on iOS Safari **after Add to Home Screen** → notification fires. Unsubscribe → row deleted. Simulate `410 Gone` → row deleted.
- **Chat (Phase 6a)**: open `/today` → SSE-stream a greeting; reply → assistant streams in 40-80 token chunks; `mark_topic_covered` lights coverage chips; `propose_wrap_up` flips phase + surfaces "I'm done"; finalize → 202 + extraction worker upserts daily_inputs (manual-wins) + journal_entries (`source='chat'`); cache invalidations populate manual mode. Idle 20m → silent auto-finalize. Crisis regex hit → CrisisCard (no LLM call). Cross-tenant `GET /api/chat/sessions/by-date/:date` → 404. Mid-stream disconnect → no partial assistant row persisted; resume cleanly.
- **Voice (Phase 6b)**: confirm OpenAI key never appears in browser DevTools network tab. Disconnect mid-call → `chat_sessions.status='abandoned'` after idle. Complete call → transcript ingested as `chat_messages` rows, then same extraction path as text chat populates `daily_inputs` + `journal_entries` with `source='chat'` and `chat_session_id` set, no schema diff from 6a.
- **Prod**: restore from `pg_dump` into a fresh DB; app runs against it. Stop backend container; Caddy returns 502; restart → recovers.
- **End-to-end smoke before launch**: signup → set timezone → answer questions → wait for daily summary → receive push reminder → complete a voice call → view history.

## Critical files

- [backend/internal/store/migrations/0001_init.sql](backend/internal/store/migrations/0001_init.sql) — entire data model + uniqueness/idempotency constraints.
- [backend/internal/httpapi/router.go](backend/internal/httpapi/router.go) — auth, rate-limit, CSRF middleware composition; security posture lives here.
- [backend/internal/realtime/openai.go](backend/internal/realtime/openai.go) — ephemeral-token chokepoint that keeps the OpenAI key off the client.
- [backend/internal/jobs/runner.go](backend/internal/jobs/runner.go) — River registration; summaries and push dispatch flow through this.
- [backend/internal/timezone/local_date.go](backend/internal/timezone/local_date.go) — TZ → local_date conversion used by every write that maps to "today".
- [frontend/src/sw/push-handler.ts](frontend/src/sw/push-handler.ts) — custom SW push + `pushsubscriptionchange` handling; iOS reliability hinges on this.

## Things deliberately NOT in v1 of v2

- Redis, Postgres RLS, Vault/SOPS — overkill for this scale.
- React Native / Capacitor — PWA + Web Push covers the requirement.
- Per-user LLM model selection — pinned default; revisit later.
- Billing / Stripe — defer until usage warrants it.
- Prometheus/Grafana — `slog` JSON + Caddy access logs + Sentry are enough.
- Migration from v1 (Streamlit/docarray). The v1 repo is stale; v2 starts empty. No data import.

## Risks to manage from day one

- **OpenAI Realtime cost** (~100× text per minute). Per-user monthly minute cap and "minutes remaining" UI must ship with the voice feature, not after.
- **iOS Web Push installability gate**. Onboarding step must explicitly tell the user to "Add to Home Screen" on iOS Safari.
- **Email deliverability**. Postmark + SPF/DKIM/DMARC configured before any external user signs up.
- **PWA service-worker staleness**. `vite-plugin-pwa` `registerType: 'autoUpdate'` plus a visible "new version — refresh" banner.
- **Stale-token replay attacks**. Magic-link single-use enforced by atomic UPDATE returning user_id; tested explicitly.

## First action when implementation begins

`git init` + initial commit (committing this `PLAN.md` as the first file), then start Phase 1 of the build phases above.
