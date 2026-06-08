-- +goose Up

-- Repair daily sessions stranded in wrapping_up by the idle sweeper.
--
-- The sweeper advanced idle sessions to wrapping_up before scheduling
-- extraction; for opener-only transcripts the extraction worker then
-- tried to mark them abandoned, but wrapping_up → abandoned was an
-- illegal phase transition and the error was discarded — so the session
-- stayed in wrapping_up ("land-it" persona mode + UI pill) forever, and
-- the completed extraction_status kept the sweeper from ever
-- re-claiming it.
--
-- Scope of the repair: daily sessions in wrapping_up whose extraction
-- already completed AND that have no real user turn. The user-turn
-- guard matters — a session that was finalized earlier in the day and
-- then resumed + wrapped up again is legitimately (wrapping_up,
-- completed) and must be left alone.
UPDATE chat_sessions s
   SET phase = 'abandoned', updated_at = now()
 WHERE s.scope = 'daily'
   AND s.phase = 'wrapping_up'
   AND s.extraction_status = 'completed'
   AND NOT EXISTS (
       SELECT 1
         FROM chat_messages m
        WHERE m.session_id = s.id
          AND m.role = 'user'
          AND m.content <> ''
   );

-- +goose Down

-- One-way data repair; nothing to restore.
SELECT 1;
