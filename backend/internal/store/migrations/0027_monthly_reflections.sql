-- +goose Up

-- Monthly reflection loop. The weekly loop answers "what makes you happy
-- in the day"; the monthly loop zooms out: "looking back, what made you
-- happy — and are you happy with your direction in life?".
--
-- Monthly day is derived, not stored: the first reflection_weekday
-- on-or-after the calendar month end hosts a COMBINED session (weekly
-- letter → monthly letter → life check-in → one chat that covers the week
-- briefly then zooms out to the month). period_start = 1st of month is
-- the canonical anchor everywhere.

-- Re-admit 'month' to the summary engine (0013 tightened the CHECK to
-- 'week' under the Energy Audit pivot; monthly returns as a hierarchical
-- synthesis over weekly artifacts, NOT raw daily entries).
ALTER TABLE summary_jobs
    DROP CONSTRAINT IF EXISTS summary_jobs_period_type_check;
ALTER TABLE summary_jobs
    ADD CONSTRAINT summary_jobs_period_type_check
    CHECK (period_type IN ('week','month'));

-- One row per (user, calendar month) — the idempotency anchor for the
-- monthly loop. Survives reflection_weekday changes and weekly replays.
--
--   week_start      the hosting weekly reflection's week_start. Re-anchors
--                   on carry-over (user misses monthly day, does it the
--                   following week).
--   direction_text  distilled at finalize: did the month move the user
--                   toward the life they want?
--   intention_text  ONE intention/theme for next month, set by the user
--                   accepting a propose_intention card (or extraction
--                   fallback). Broader than a weekly tiny goal.
--   ratings         life check-in jsonb map domain_key → 0..10
--                   (PWI format: one item per domain, end-defined scale).
--                   NULL until the user submits; skippable.
CREATE TABLE monthly_reflections (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    month_start      date        NOT NULL,
    month_end        date        NOT NULL,
    week_start       date,
    chat_session_id  uuid        REFERENCES chat_sessions(id) ON DELETE SET NULL,
    direction_text   text        NOT NULL DEFAULT '',
    intention_text   text        NOT NULL DEFAULT '',
    intention_set_at timestamptz,
    ratings          jsonb,
    ratings_set_at   timestamptz,
    completed_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, month_start),
    CHECK (month_end >= month_start)
);

CREATE INDEX monthly_reflections_user_month_idx
    ON monthly_reflections (user_id, month_start DESC);

-- Combined-session pin: set once at weekly-session creation when the week
-- hosts a monthly reflection. Stable for the session's lifetime (no
-- recompute flapping mid-conversation); the extraction worker reads it as
-- the month anchor without user-tz recomputation.
ALTER TABLE chat_sessions ADD COLUMN month_period_start date;
ALTER TABLE chat_sessions ADD CONSTRAINT chat_sessions_month_scope
    CHECK (month_period_start IS NULL OR scope = 'weekly');

-- +goose Down
ALTER TABLE chat_sessions DROP CONSTRAINT chat_sessions_month_scope;
ALTER TABLE chat_sessions DROP COLUMN IF EXISTS month_period_start;
DROP TABLE IF EXISTS monthly_reflections;
-- Month rows must go before the CHECK re-tightens.
DELETE FROM summaries WHERE period_type = 'month';
DELETE FROM summary_jobs WHERE period_type = 'month';
ALTER TABLE summary_jobs DROP CONSTRAINT IF EXISTS summary_jobs_period_type_check;
ALTER TABLE summary_jobs ADD CONSTRAINT summary_jobs_period_type_check
    CHECK (period_type = 'week');
