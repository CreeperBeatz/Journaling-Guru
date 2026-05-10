-- +goose Up
-- Add a flag distinguishing "follow the browser's timezone automatically"
-- (true, default) from "user pinned a specific zone" (false). The stored
-- users.timezone column keeps its meaning and is still the only thing
-- workers read; this flag only controls whether the API auto-syncs that
-- column from the browser-detected tz on every /api/me load.
ALTER TABLE users
    ADD COLUMN timezone_auto boolean NOT NULL DEFAULT true;

-- +goose Down
ALTER TABLE users DROP COLUMN timezone_auto;
