-- +goose Up

-- chat_sessions: one reflective AI-driven session per (user, local_date).
-- Idempotency anchor matches daily_inputs — one session per day, mode is
-- mutable (text in Phase 6a, voice in Phase 6b reuses this row by setting
-- mode='voice' and openai_session_id). Voice does not get its own table.
--
-- phase tracks the conversational arc and gates the UI:
--   greeting     — fresh session, no user reply yet
--   exploring    — back-and-forth happening
--   wrapping_up  — model proposed wrap or user signaled
--   finalized    — extraction complete, ended_at + finalized_at set
--   abandoned    — terminal-not-extracted (idle sweeper writes 'wrapping_up'
--                  then enqueues extraction; only reaches here if the
--                  extraction job is permanently failed AND the user
--                  never returns).
--
-- last_activity_at is the idle-sweeper anchor; updated on every user turn,
-- assistant turn, or tool call.
--
-- extraction_status drives the "filling out your check-in…" UI between
-- finalize and the worker landing. Lifecycle parallels chat_extraction_jobs.
--
-- chat_model / extraction_model are persisted per session so a model
-- change in env doesn't make replays inconsistent with the original run.
CREATE TABLE chat_sessions (
    id                 uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date         date        NOT NULL,
    mode               text        NOT NULL DEFAULT 'text'
        CHECK (mode IN ('text','voice')),
    phase              text        NOT NULL DEFAULT 'greeting'
        CHECK (phase IN ('greeting','exploring','wrapping_up','finalized','abandoned')),
    chat_model         text        NOT NULL DEFAULT '',
    extraction_model   text        NOT NULL DEFAULT '',
    openai_session_id  text,
    started_at         timestamptz NOT NULL DEFAULT now(),
    last_activity_at   timestamptz NOT NULL DEFAULT now(),
    ended_at           timestamptz,
    finalized_at       timestamptz,
    extraction_status  text        NOT NULL DEFAULT 'idle'
        CHECK (extraction_status IN ('idle','pending','running','completed','failed')),
    extraction_error   text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE chat_sessions
    ADD CONSTRAINT chat_sessions_user_date_unique
    UNIQUE (user_id, local_date);

-- Idle sweeper scans this — partial index keeps it tiny once finalized
-- sessions accumulate.
CREATE INDEX chat_sessions_idle_idx
    ON chat_sessions (last_activity_at)
    WHERE phase IN ('greeting','exploring','wrapping_up');

CREATE INDEX chat_sessions_user_date_idx
    ON chat_sessions (user_id, local_date DESC);

-- chat_messages: full transcript of one session, ordered by `seq` (assigned
-- by the store under FOR UPDATE on the session row to keep ordering
-- deterministic across concurrent writers).
--
-- role: user | assistant | tool | system_event
--   - user/assistant carry `content` (plain text or markdown).
--   - assistant rows carrying tool-call output also set tool_name +
--     tool_args. The follow-up tool row carries tool_result.
--   - system_event is for sweeper-injected entries ("idle finalize") and
--     UI signals ("user_dismissed_crisis"). Visible to the prompt builder
--     for context but rendered differently in the UI.
--
-- Only complete assistant turns are persisted (the streaming handler
-- appends the row after the SSE stream closes). Mid-stream connection
-- drops do NOT persist — by design; resume re-asks.
CREATE TABLE chat_messages (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id   uuid        NOT NULL REFERENCES chat_sessions(id) ON DELETE CASCADE,
    seq          integer     NOT NULL,
    role         text        NOT NULL
        CHECK (role IN ('user','assistant','tool','system_event')),
    content      text        NOT NULL DEFAULT '',
    tool_name    text,
    tool_args    jsonb,
    tool_result  jsonb,
    token_in     integer     NOT NULL DEFAULT 0,
    token_out    integer     NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE chat_messages
    ADD CONSTRAINT chat_messages_session_seq_unique
    UNIQUE (session_id, seq);

CREATE INDEX chat_messages_session_seq_idx
    ON chat_messages (session_id, seq ASC);

-- Widen journal_entries.source CHECK to admit 'chat'. Drop-and-readd is
-- the only way to mutate a CHECK in Postgres; on this small dataset the
-- access exclusive lock is instant.
ALTER TABLE journal_entries DROP CONSTRAINT IF EXISTS journal_entries_source_check;
ALTER TABLE journal_entries
    ADD CONSTRAINT journal_entries_source_check
    CHECK (source IN ('text','voice','chat'));

-- Repurpose voice_session_id → chat_session_id. The column was added in
-- 0002_journal.sql as a bare uuid (no FK) waiting for Phase 6 to land
-- the parent table. Now we have it: FK to chat_sessions, ON DELETE
-- SET NULL so cascading a session delete doesn't take entries with it.
ALTER TABLE journal_entries RENAME COLUMN voice_session_id TO chat_session_id;
ALTER TABLE journal_entries
    ADD CONSTRAINT journal_entries_chat_session_fk
    FOREIGN KEY (chat_session_id) REFERENCES chat_sessions(id) ON DELETE SET NULL;

-- chat_extraction_jobs: parallels summary_jobs / emotion_classify_jobs
-- so the existing dispatcher tick in cmd/worker can drain it with the
-- same FOR UPDATE SKIP LOCKED pattern. UNIQUE (session_id) — one
-- extraction per session ever; ON CONFLICT DO UPDATE re-arms a failed
-- one (the Regenerate path).
CREATE TABLE chat_extraction_jobs (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id  uuid        NOT NULL UNIQUE REFERENCES chat_sessions(id) ON DELETE CASCADE,
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fire_at     timestamptz NOT NULL DEFAULT now(),
    fired_at    timestamptz,
    status      text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','completed','skipped','failed')),
    attempts    integer     NOT NULL DEFAULT 0,
    last_error  text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX chat_extraction_jobs_due_idx
    ON chat_extraction_jobs (fire_at)
    WHERE status = 'pending';

-- +goose Down

DROP TABLE IF EXISTS chat_extraction_jobs;

ALTER TABLE journal_entries DROP CONSTRAINT IF EXISTS journal_entries_chat_session_fk;
ALTER TABLE journal_entries RENAME COLUMN chat_session_id TO voice_session_id;

ALTER TABLE journal_entries DROP CONSTRAINT IF EXISTS journal_entries_source_check;
ALTER TABLE journal_entries
    ADD CONSTRAINT journal_entries_source_check
    CHECK (source IN ('text','voice'));

DROP TABLE IF EXISTS chat_messages;
DROP TABLE IF EXISTS chat_sessions;
