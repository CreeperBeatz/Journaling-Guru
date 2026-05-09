-- +goose Up

-- Phase 6b: opt-in "Finish & overwrite" finalize. The default extraction
-- path is manual-wins (MergeFromExtraction COALESCE / CASE WHEN empty);
-- when the user explicitly chooses the overwrite affordance, the worker
-- branches to OverwriteFromExtraction which clobbers daily_inputs text +
-- mood from the session. journal_entries already overwrite via
-- UpsertFromChat — this flag does not alter that behavior.
--
-- Default false so any in-flight job rows scheduled before this migration
-- (or by the idle sweeper, which never wants to clobber manual edits)
-- keep manual-wins semantics.
ALTER TABLE chat_extraction_jobs
    ADD COLUMN overwrite boolean NOT NULL DEFAULT false;

-- +goose Down

ALTER TABLE chat_extraction_jobs DROP COLUMN IF EXISTS overwrite;
