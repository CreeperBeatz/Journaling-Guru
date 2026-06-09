-- +goose Up

-- Optional per-domain explanation for the life check-in: the FE shows
-- one card per domain (slider + "want to say why?" free text). Keyed by
-- the same stable domain keys as ratings; absent key = no note.
ALTER TABLE monthly_reflections ADD COLUMN rating_notes jsonb;

-- +goose Down
ALTER TABLE monthly_reflections DROP COLUMN IF EXISTS rating_notes;
