-- +goose Up

-- daily_inputs holds the user-provided "check-in" for one calendar day:
-- a 1-10 mood score, a list of emotions felt, and freeform notes.
-- Unlike journal_entries (which are per-question), there is exactly one
-- daily_inputs row per (user, local_date) — the same UNIQUE pattern
-- summaries uses for its idempotency anchor.
--
-- Why a separate table (not extra columns on users or rows in
-- journal_entries):
--   - Per-day, not per-question — a question-row would conflate user
--     content with system-tracked metadata.
--   - Lifecycle differs from journal_entries (often a user logs mood
--     and notes without answering any questions, and vice-versa).
--   - The mood/emotions are the source of truth for the SummariesPage
--     stats panel; isolating them keeps those queries fast and the
--     `summaries` table concerned only with LLM output.
--
-- mood_score is NULL when the user hasn't set it (so charts can skip
-- the day rather than imputing zero). 1=very negative, 10=very positive.
-- emotions is a normalized lower-case string array; deduplication
-- happens at the application layer before the write.
-- notes is freeform text fed into the daily-summary LLM prompt as
-- additional context; never shown verbatim in summaries.
CREATE TABLE daily_inputs (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date  date        NOT NULL,
    mood_score  integer
        CHECK (mood_score IS NULL OR (mood_score >= 1 AND mood_score <= 10)),
    emotions    jsonb       NOT NULL DEFAULT '[]'::jsonb,
    notes       text        NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE daily_inputs
    ADD CONSTRAINT daily_inputs_user_date_unique
    UNIQUE (user_id, local_date);

-- Stats panel: mood-by-date over a sliding window. Newest first.
CREATE INDEX daily_inputs_user_date_idx
    ON daily_inputs (user_id, local_date DESC);

-- +goose Down
DROP TABLE IF EXISTS daily_inputs;
