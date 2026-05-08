-- +goose Up

-- Reshape daily_inputs to match the spec's DailyEntry. Under the
-- Energy Audit pivot the day is described by five fixed fields:
--
--   mood              1..3   (sad / neutral / happy)
--   drained_text      free text — what drained the user
--   charged_text      free text — what charged the user
--   gratitude_text    free text — not analyzed
--   reflection_text   free text — not analyzed (was 'notes')
--
-- The drainer/charger tags themselves live in daily_entry_tags
-- (migration 0011), keyed by (user_id, local_date, tag_id, role).
--
-- The previous mood_score (1-10) and emotions_text/classified_emotions
-- columns are dropped. Dev DB only — no data migration; reseed.

ALTER TABLE daily_inputs DROP COLUMN mood_score;
ALTER TABLE daily_inputs DROP COLUMN emotions_text;
ALTER TABLE daily_inputs DROP COLUMN classified_emotions;

ALTER TABLE daily_inputs
    ADD COLUMN mood smallint
        CHECK (mood IS NULL OR (mood BETWEEN 1 AND 3)),
    ADD COLUMN drained_text   text NOT NULL DEFAULT '',
    ADD COLUMN charged_text   text NOT NULL DEFAULT '',
    ADD COLUMN gratitude_text text NOT NULL DEFAULT '',
    ADD COLUMN backfilled     boolean NOT NULL DEFAULT false,
    ADD COLUMN edited_at      timestamptz;

ALTER TABLE daily_inputs RENAME COLUMN notes TO reflection_text;

-- emotion_classify_jobs is no longer driven by anything — drop it. The
-- worker is retired in the same wave as this migration.
DROP TABLE IF EXISTS emotion_classify_jobs;

-- +goose Down
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
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT emotion_classify_jobs_user_date_unique UNIQUE (user_id, local_date)
);
CREATE INDEX emotion_classify_jobs_due_idx
    ON emotion_classify_jobs (fire_at)
    WHERE status = 'pending';

ALTER TABLE daily_inputs RENAME COLUMN reflection_text TO notes;
ALTER TABLE daily_inputs DROP COLUMN edited_at;
ALTER TABLE daily_inputs DROP COLUMN backfilled;
ALTER TABLE daily_inputs DROP COLUMN gratitude_text;
ALTER TABLE daily_inputs DROP COLUMN charged_text;
ALTER TABLE daily_inputs DROP COLUMN drained_text;
ALTER TABLE daily_inputs DROP COLUMN mood;
ALTER TABLE daily_inputs
    ADD COLUMN mood_score          integer
        CHECK (mood_score IS NULL OR (mood_score >= 1 AND mood_score <= 10)),
    ADD COLUMN emotions_text       text  NOT NULL DEFAULT '',
    ADD COLUMN classified_emotions jsonb NOT NULL DEFAULT '[]'::jsonb;
