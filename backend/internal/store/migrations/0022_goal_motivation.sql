-- +goose Up

-- Capture the user's own words on the WHY of a goal at creation time so
-- it survives past the chat transcript. Three columns mirror the three
-- gating questions the weekly-reflection companion asks before calling
-- propose_goal: why_matters / if_followed / if_not_followed. The Goals
-- page surfaces them under the title; the daily check-in can re-quote
-- them when motivation is wavering.
--
-- All TEXT NOT NULL DEFAULT '' so manual-create paths (GoalsPage's
-- CreateGoalCard, SMART shaper commit_goal tool) keep working without
-- prompting the user for these answers — only the weekly propose_goal
-- flow fills them today.

ALTER TABLE goals
    ADD COLUMN why_matters     text NOT NULL DEFAULT '',
    ADD COLUMN if_followed     text NOT NULL DEFAULT '',
    ADD COLUMN if_not_followed text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE goals
    DROP COLUMN if_not_followed,
    DROP COLUMN if_followed,
    DROP COLUMN why_matters;
