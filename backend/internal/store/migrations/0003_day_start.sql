-- +goose Up

-- day_start_minutes shifts the boundary between calendar days for the
-- "what counts as today" calculation. 0 = midnight (default UTC behavior),
-- 360 = 06:00 (a journaler who writes at 1am is still answering for
-- yesterday). Stored as minutes-since-midnight, [0, 1440).
ALTER TABLE users
    ADD COLUMN day_start_minutes integer NOT NULL DEFAULT 360
        CHECK (day_start_minutes >= 0 AND day_start_minutes < 1440);

-- +goose Down
ALTER TABLE users DROP COLUMN day_start_minutes;
