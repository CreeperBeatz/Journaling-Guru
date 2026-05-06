-- +goose Up

-- summaries holds the LLM-generated reflection for one (user, period) pair.
-- (user_id, period_type, period_start) is the idempotency key — the worker
-- relies on the UNIQUE constraint to make retries safe.
--
-- metadata is structured stats extracted from the content (daily) or
-- aggregated from constituent periods (weekly/monthly/yearly):
--   { emotions: ["curious","frustrated"],
--     mood_score: 7,         -- 1..10
--     mood_label: "positive",
--     topics: ["work","family"],
--     entry_count: 4 }
-- Stored as jsonb so the SummariesPage stats panel can aggregate cheaply
-- (mood sparkline = SELECT period_start, metadata->>'mood_score' …).
CREATE TABLE summaries (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period_type       text        NOT NULL
        CHECK (period_type IN ('day','week','month','year')),
    period_start      date        NOT NULL,
    period_end        date        NOT NULL,
    body              text        NOT NULL,
    metadata          jsonb       NOT NULL DEFAULT '{}'::jsonb,
    model             text        NOT NULL,
    prompt_tokens     integer     NOT NULL DEFAULT 0,
    completion_tokens integer     NOT NULL DEFAULT 0,
    generated_at      timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE summaries
    ADD CONSTRAINT summaries_user_period_unique
    UNIQUE (user_id, period_type, period_start);

-- SummariesPage list: filter by user+period, newest first.
CREATE INDEX summaries_user_period_idx
    ON summaries (user_id, period_type, period_start DESC);

-- summary_jobs is the scheduler queue. One row = "summary X should run at
-- fire_at." Lazy-seeded on a user's first journal write per period; the
-- worker re-enqueues the next period's row after each successful fire
-- (subject to a dormancy guard for day/week/month — yearly always re-arms).
--
-- status lifecycle:
--   pending  → scheduled, fire_at not yet reached (or reached but unclaimed).
--   claimed  → dispatcher tick has handed it to a River worker.
--   completed → summary written.
--   skipped  → period had no entries; no LLM call made.
--   failed   → River exhausted retries; do not auto re-arm next period.
--
-- attempts increments at claim time. When the River worker sees attempt ==
-- max_attempts on a failure path, it marks failed instead of pending.
CREATE TABLE summary_jobs (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period_type  text        NOT NULL
        CHECK (period_type IN ('day','week','month','year')),
    period_start date        NOT NULL,
    fire_at      timestamptz NOT NULL,
    fired_at     timestamptz,
    status       text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','completed','skipped','failed')),
    attempts     integer     NOT NULL DEFAULT 0,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE summary_jobs
    ADD CONSTRAINT summary_jobs_user_period_unique
    UNIQUE (user_id, period_type, period_start);

-- Dispatcher tick: claim due rows. Partial index keeps it tiny.
CREATE INDEX summary_jobs_due_idx
    ON summary_jobs (fire_at)
    WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS summary_jobs;
DROP TABLE IF EXISTS summaries;
