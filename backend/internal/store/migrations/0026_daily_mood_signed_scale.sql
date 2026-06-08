-- +goose Up

-- Migrate daily mood from the 1..3 scale (sad/neutral/happy) to a
-- signed 5-point scale: -2 (very bad) .. +2 (very good), 0=neutral.
--
-- Existing data maps conservatively (no synthetic extremes):
--   1 -> -1, 2 -> 0, 3 -> +1   (i.e. mood - 2)
-- The old scale couldn't distinguish "bad" from "very bad", so old
-- rows land on the moderate points; -2/+2 only appear going forward.
--
-- The old CHECK (mood BETWEEN 1 AND 3) would reject -1 mid-update,
-- so drop it before the remap, then install the widened CHECK.

ALTER TABLE daily_inputs DROP CONSTRAINT IF EXISTS daily_inputs_mood_check;

UPDATE daily_inputs
   SET mood = mood - 2
 WHERE mood IS NOT NULL;

ALTER TABLE daily_inputs
    ADD CONSTRAINT daily_inputs_mood_check
        CHECK (mood IS NULL OR (mood BETWEEN -2 AND 2));

-- +goose Down

-- Lossy reverse: the new extremes collapse into the old ends.
--   -2,-1 -> 1 ; 0 -> 2 ; +1,+2 -> 3

ALTER TABLE daily_inputs DROP CONSTRAINT IF EXISTS daily_inputs_mood_check;

UPDATE daily_inputs
   SET mood = CASE
                  WHEN mood <= -1 THEN 1
                  WHEN mood = 0   THEN 2
                  ELSE 3
              END
 WHERE mood IS NOT NULL;

ALTER TABLE daily_inputs
    ADD CONSTRAINT daily_inputs_mood_check
        CHECK (mood IS NULL OR (mood BETWEEN 1 AND 3));
