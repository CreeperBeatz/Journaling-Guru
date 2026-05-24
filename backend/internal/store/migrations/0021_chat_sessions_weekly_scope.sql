-- +goose Up

-- Weekly reflection becomes a 2-step wizard: read the letter, then chat.
-- Step 2 is a weekly-scoped chat session that reuses the existing chat
-- infrastructure (SSE streaming, opener, wrap-up) but carries a different
-- system prompt, different tools, and skips the daily extraction pipeline.
--
-- chat_sessions grows two columns to disambiguate the two scopes:
--   scope          'daily' | 'weekly'. Default 'daily' so existing rows
--                  carry the right meaning without backfill.
--   period_start   week_start date when scope='weekly'; NULL for daily.
--
-- The daily uniqueness invariant (user_id, local_date) is preserved as a
-- partial unique on scope='daily'. Weekly rows are uniqued by
-- (user_id, period_start). Weekly rows still set local_date = period_start
-- so the existing code paths that read local_date for "as-of" lookups
-- (goal listing, prompt context) keep working unchanged.

ALTER TABLE chat_sessions
    ADD COLUMN scope        text NOT NULL DEFAULT 'daily'
        CHECK (scope IN ('daily','weekly')),
    ADD COLUMN period_start date;

ALTER TABLE chat_sessions
    ADD CONSTRAINT chat_sessions_scope_anchor CHECK (
        (scope = 'daily'  AND period_start IS NULL) OR
        (scope = 'weekly' AND period_start IS NOT NULL)
    );

-- Replace the global unique with two partial uniques, one per scope.
ALTER TABLE chat_sessions
    DROP CONSTRAINT chat_sessions_user_date_unique;

CREATE UNIQUE INDEX chat_sessions_user_date_daily_uniq
    ON chat_sessions (user_id, local_date)
    WHERE scope = 'daily';

CREATE UNIQUE INDEX chat_sessions_user_period_weekly_uniq
    ON chat_sessions (user_id, period_start)
    WHERE scope = 'weekly';

CREATE INDEX chat_sessions_user_period_idx
    ON chat_sessions (user_id, period_start DESC)
    WHERE scope = 'weekly';

-- Link weekly_reflections to its chat session for one-shot lookup. FK is
-- nullable + ON DELETE SET NULL so deleting a chat session never cascades
-- through reflection state.
ALTER TABLE weekly_reflections
    ADD COLUMN chat_session_id uuid REFERENCES chat_sessions(id) ON DELETE SET NULL;

CREATE INDEX weekly_reflections_chat_session_idx
    ON weekly_reflections (chat_session_id)
    WHERE chat_session_id IS NOT NULL;

-- Wizard shrinks from 4 steps back to 2 (Letter → Chat). Squash any
-- in-flight rows past step 1 into the new step 2 (the chat). Completed
-- rows are unaffected for navigation; History only reads completed_at.
ALTER TABLE weekly_reflections
    DROP CONSTRAINT weekly_reflections_step_check;

UPDATE weekly_reflections SET step = 2 WHERE step >= 2;
UPDATE weekly_reflections SET step = 1 WHERE step < 1;

ALTER TABLE weekly_reflections
    ADD CONSTRAINT weekly_reflections_step_check
        CHECK (step BETWEEN 1 AND 2);

-- +goose Down

ALTER TABLE weekly_reflections
    DROP CONSTRAINT weekly_reflections_step_check;

-- Best-effort: leave step in [1,2] which is valid under the old 1..4
-- constraint. We can't reconstruct the original Pattern/Goal/Shape cursor.
ALTER TABLE weekly_reflections
    ADD CONSTRAINT weekly_reflections_step_check
        CHECK (step BETWEEN 1 AND 4);

DROP INDEX IF EXISTS weekly_reflections_chat_session_idx;
ALTER TABLE weekly_reflections
    DROP COLUMN IF EXISTS chat_session_id;

DROP INDEX IF EXISTS chat_sessions_user_period_idx;
DROP INDEX IF EXISTS chat_sessions_user_period_weekly_uniq;
DROP INDEX IF EXISTS chat_sessions_user_date_daily_uniq;

ALTER TABLE chat_sessions
    ADD CONSTRAINT chat_sessions_user_date_unique
    UNIQUE (user_id, local_date);

ALTER TABLE chat_sessions
    DROP CONSTRAINT chat_sessions_scope_anchor;

ALTER TABLE chat_sessions
    DROP COLUMN IF EXISTS period_start,
    DROP COLUMN IF EXISTS scope;
