-- +goose Up

-- Per-user prompt list. position drives display order; we re-pack on
-- reorder/archive so gaps don't accumulate. archived_at is a soft delete
-- so we preserve the FK from journal_entries.question_id.
CREATE TABLE questions (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    prompt       text        NOT NULL,
    position     integer     NOT NULL,
    archived_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- DEFERRABLE so a multi-row reorder UPDATE inside a transaction doesn't trip
-- the constraint mid-statement.
ALTER TABLE questions
    ADD CONSTRAINT questions_user_position_unique
    UNIQUE (user_id, position) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX questions_user_active_idx
    ON questions (user_id, position)
    WHERE archived_at IS NULL;

-- One journal entry per (user, question, day-in-user-tz). local_date is
-- computed server-side from the user's IANA timezone at write time, never
-- from the client clock.
--
-- voice_session_id is nullable now; the FK is added in Phase 6 when the
-- voice_sessions table lands.
CREATE TABLE journal_entries (
    id                uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    question_id       uuid        NOT NULL REFERENCES questions(id),
    local_date        date        NOT NULL,
    body              text        NOT NULL,
    source            text        NOT NULL DEFAULT 'text'
        CHECK (source IN ('text', 'voice')),
    voice_session_id  uuid,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE journal_entries
    ADD CONSTRAINT journal_entries_user_question_date_unique
    UNIQUE (user_id, question_id, local_date);

-- HistoryView lists dates with entries; this index serves both that and the
-- DailyEntry per-day fetch (filter by user, range/equality on local_date).
CREATE INDEX journal_entries_user_date_idx
    ON journal_entries (user_id, local_date DESC);

-- +goose Down
DROP TABLE IF EXISTS journal_entries;
DROP TABLE IF EXISTS questions;
