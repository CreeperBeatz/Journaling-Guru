-- +goose Up

-- weekly_reflections holds the per-week wizard state for the interactive
-- weekly reflection (PLAN.md Phase 7). One row per user per week_start.
--
--   surprise_text     free-text answer to "did anything surprise you?".
--                     Previously squatted on daily_inputs.reflection_text.
--   step              wizard cursor (1..3) so a refresh resumes at the
--                     same card. Bumped via PATCH from the FE.
--   goal_notes        per-mid-flight-goal "how's it going so far?" notes,
--                     keyed by goal_id (string) → free text. Not used to
--                     resolve goals — only to record context for History.
--   completed_at      set by POST /complete; non-NULL flips the page to
--                     the read-only Done view for the rest of the week.
--
-- Idempotency anchor: UNIQUE (user_id, week_start). Lazy-created on
-- /start; subsequent /start calls are no-ops (ON CONFLICT DO NOTHING).

CREATE TABLE weekly_reflections (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_start    date        NOT NULL,
    week_end      date        NOT NULL,
    surprise_text text        NOT NULL DEFAULT '',
    step          smallint    NOT NULL DEFAULT 1
        CHECK (step BETWEEN 1 AND 3),
    goal_notes    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    completed_at  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, week_start),
    CHECK (week_end >= week_start)
);

-- History view loads by week_start range; index keeps the lookup cheap.
CREATE INDEX weekly_reflections_user_week_idx
    ON weekly_reflections (user_id, week_start DESC);

-- +goose Down
DROP TABLE IF EXISTS weekly_reflections;
