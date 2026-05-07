-- +goose Up

-- chat_sessions.covered_question_ids replaces the inline mark_topic_covered
-- tool with an authoritative set computed by a post-turn LLM classifier.
-- The streaming handler runs the classifier after each assistant turn,
-- overwrites this column with the result, and emits a coverage_update SSE
-- event so the FE chip strip mirrors the persisted truth across reloads
-- and multi-tab.
--
-- text[] (not jsonb) — the data is a flat list of uuid strings; array
-- semantics are simpler for SET equality and don't pull in jsonb operator
-- ergonomics we don't need.
ALTER TABLE chat_sessions
    ADD COLUMN covered_question_ids text[] NOT NULL DEFAULT ARRAY[]::text[];

-- +goose Down

ALTER TABLE chat_sessions DROP COLUMN IF EXISTS covered_question_ids;
