-- +goose Up

-- Phase 4.1 follow-up: emotions become free-text with an async LLM
-- classification step into Plutchik's wheel (8 base × 3 intensities).
-- The 24-chip palette is gone on the frontend; in its place a single
-- Textarea writes raw user text into emotions_text. A new River-backed
-- worker reads emotions_text and produces classified_emotions of shape
-- [{base, subtype, raw_phrase}], which is what the SummariesPage stats
-- panel and SummaryDetail metadata pills now consume.
--
-- Existing daily_inputs.emotions data is dropped — single-user dev box,
-- no migration of values needed.
ALTER TABLE daily_inputs DROP COLUMN emotions;
ALTER TABLE daily_inputs
    ADD COLUMN emotions_text       text  NOT NULL DEFAULT '',
    ADD COLUMN classified_emotions jsonb NOT NULL DEFAULT '[]'::jsonb;

-- emotion_classify_jobs is the queue the api writes on every daily-input
-- upsert with non-empty emotions_text. Same lifecycle vocabulary as
-- summary_jobs (pending → claimed → completed/skipped/failed) so the
-- dispatcher tick in cmd/worker can drain both queues with the same
-- atomic FOR UPDATE SKIP LOCKED claim pattern.
--
-- One row per (user, local_date) — re-saving the day resets the row to
-- pending unless it's currently in flight.
CREATE TABLE emotion_classify_jobs (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date  date        NOT NULL,
    fire_at     timestamptz NOT NULL DEFAULT now(),
    fired_at    timestamptz,
    status      text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','completed','skipped','failed')),
    attempts    integer     NOT NULL DEFAULT 0,
    last_error  text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE emotion_classify_jobs
    ADD CONSTRAINT emotion_classify_jobs_user_date_unique
    UNIQUE (user_id, local_date);

CREATE INDEX emotion_classify_jobs_due_idx
    ON emotion_classify_jobs (fire_at)
    WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS emotion_classify_jobs;
ALTER TABLE daily_inputs DROP COLUMN emotions_text;
ALTER TABLE daily_inputs DROP COLUMN classified_emotions;
ALTER TABLE daily_inputs ADD COLUMN emotions jsonb NOT NULL DEFAULT '[]'::jsonb;
