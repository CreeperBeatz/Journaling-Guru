-- +goose Up

-- memory_extraction_jobs: scheduling source of truth for the per-day
-- memory reconciliation pass. Parallels summary_jobs /
-- chat_extraction_jobs so the dispatcher tick in cmd/worker drains it
-- with the same FOR UPDATE SKIP LOCKED pattern and inserts a River
-- MemoryExtractionArgs job.
--
-- Idempotency anchor: UNIQUE (user_id, local_date) — exactly one memory
-- pass per user-day. Lazy-seeded (ON CONFLICT DO NOTHING) from entry /
-- daily-input writes and from chat extraction apply; fires at day close
-- (next day at day_start+30 in user tz, same instant the daily summary
-- used to fire).
CREATE TABLE memory_extraction_jobs (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date  date        NOT NULL,
    fire_at     timestamptz NOT NULL,
    fired_at    timestamptz,
    status      text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','completed','skipped','failed')),
    attempts    integer     NOT NULL DEFAULT 0,
    last_error  text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, local_date)
);

CREATE INDEX memory_extraction_jobs_due_idx
    ON memory_extraction_jobs (fire_at)
    WHERE status = 'pending';

-- +goose Down

DROP TABLE IF EXISTS memory_extraction_jobs;
