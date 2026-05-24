-- +goose Up

-- Wizard now has four steps: Letter → Pattern+Surprise → GoalReview →
-- ShapeNext. Previously it was three (Pattern was step 1). Bump the
-- CHECK constraint and shift any in-flight rows forward by one so an
-- existing wizard cursor doesn't suddenly mean a different card.
--
-- The default for new rows becomes step=1 (Letter) — the new starting
-- card. Completed reflections (completed_at IS NOT NULL) keep their
-- final step value bumped one slot too, so the "last card I saw" hint
-- in History stays semantically consistent.

ALTER TABLE weekly_reflections
    DROP CONSTRAINT weekly_reflections_step_check;

UPDATE weekly_reflections
   SET step = step + 1
 WHERE step BETWEEN 1 AND 3;

ALTER TABLE weekly_reflections
    ADD CONSTRAINT weekly_reflections_step_check
        CHECK (step BETWEEN 1 AND 4);

-- +goose Down

-- Best-effort reverse: shift in-flight rows back down. Rows that were
-- created at step=4 (new Letter card with one extra step ahead) become
-- step=3, which is valid under the old constraint.
ALTER TABLE weekly_reflections
    DROP CONSTRAINT weekly_reflections_step_check;

UPDATE weekly_reflections
   SET step = step - 1
 WHERE step BETWEEN 2 AND 4;

UPDATE weekly_reflections
   SET step = 1
 WHERE step < 1;

ALTER TABLE weekly_reflections
    ADD CONSTRAINT weekly_reflections_step_check
        CHECK (step BETWEEN 1 AND 3);
