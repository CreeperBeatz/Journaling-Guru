-- +goose Up

-- user_memories: durable, LLM-reconciled facts about the user's life
-- ("sister Mara lives nearby", "started a new job at Acme in June"),
-- injected into chat sessions so the agent remembers the user across
-- days. Source of truth for extraction is the day's canonical journal
-- record (journal_entries + daily_inputs) — NOT raw chat transcripts —
-- so manual edits flow through manual-wins before ever reaching memory.
--
-- Lifecycle is Mem0-style reconciliation with soft-supersede lineage:
--   - ADD inserts a new active row.
--   - UPDATE inserts a replacement active row and flips the old one to
--     status='superseded' with superseded_by pointing at the new row.
--   - DELETE flips status='deleted'.
-- History rows are never hard-removed (except account-delete cascade),
-- so "back when you were at X" stays reconstructable.
--
-- pinned=true means user-edited or user-created (source='user'). The
-- reconciliation worker MUST NOT update or delete pinned rows —
-- manual-wins, same contract as daily_inputs.MergeFromExtraction and
-- entries.UpsertIfAbsent.
CREATE TABLE user_memories (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category          text        NOT NULL
        CHECK (category IN (
            'identity','relationships','work','health',
            'preferences','goals','routines','other')),
    content           text        NOT NULL
        CHECK (content <> '' AND char_length(content) <= 500),
    status            text        NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','superseded','deleted')),
    pinned            boolean     NOT NULL DEFAULT false,
    source            text        NOT NULL DEFAULT 'extraction'
        CHECK (source IN ('extraction','user')),
    -- lineage: the row that replaced this one on UPDATE. NULL for
    -- active and deleted heads.
    superseded_by     uuid        REFERENCES user_memories(id) ON DELETE SET NULL,
    -- the user-local day whose journal record produced / last touched
    -- this memory. NULL for user-created rows.
    source_local_date date,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

-- Hot path: "load all active memories for a user" at chat session
-- start. Partial index stays tight as superseded/deleted history
-- accumulates.
CREATE INDEX user_memories_active_idx
    ON user_memories (user_id, category)
    WHERE status = 'active';

-- Management UI listing + lineage walks.
CREATE INDEX user_memories_user_status_idx
    ON user_memories (user_id, status, created_at DESC);

-- +goose Down

DROP TABLE IF EXISTS user_memories;
