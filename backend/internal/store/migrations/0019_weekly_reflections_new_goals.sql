-- +goose Up

-- new_goal_ids tracks which goals were shaped *during* the wizard's
-- Card 3. The FE pushes each ID via PATCH /api/reflection/this-week
-- after the commit_goal CTA succeeds. The Done page uses this to split
-- active_goals into "Active" (carried over) vs "New" (this reflection).
--
-- A pre-existing goal could otherwise have start_date >= week_start
-- (created earlier in the same week via the goals side menu) and be
-- incorrectly bucketed as "new" by date alone — that's the bug this
-- column fixes.

ALTER TABLE weekly_reflections
    ADD COLUMN new_goal_ids jsonb NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE weekly_reflections DROP COLUMN new_goal_ids;
