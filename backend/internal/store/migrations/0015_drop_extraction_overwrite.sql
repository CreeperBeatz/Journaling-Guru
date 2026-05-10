-- +goose Up
-- The Talk-mode redesign drops the keep/replace finalize fork: extraction
-- now silently merges (LLM-merge for non-empty conflicts, manual fields
-- preserved otherwise). The chat_extraction_jobs.overwrite column is no
-- longer read by the worker.
ALTER TABLE chat_extraction_jobs DROP COLUMN IF EXISTS overwrite;

-- +goose Down
ALTER TABLE chat_extraction_jobs
    ADD COLUMN overwrite boolean NOT NULL DEFAULT false;
