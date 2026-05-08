package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// EntryStore reads and writes journal_entries. Every method is scoped by
// user_id; the UNIQUE (user_id, question_id, local_date) constraint
// enforces the "one answer per question per day" invariant at the DB.
type EntryStore struct {
	DB *pgxpool.Pool
}

func NewEntryStore(db *pgxpool.Pool) *EntryStore { return &EntryStore{DB: db} }

// ErrEntryQuestionMissing is returned by Upsert when the question_id
// doesn't belong to the caller (or doesn't exist). Surfaced as 404.
var ErrEntryQuestionMissing = errors.New("question not found")

// ErrEntryNotFound is returned by UpdateBody when the entry id doesn't
// belong to the caller. Surfaced as 404.
var ErrEntryNotFound = errors.New("entry not found")

const entryColumns = `id, user_id, question_id,
    to_char(local_date, 'YYYY-MM-DD') AS local_date,
    body, source, chat_session_id, created_at, updated_at`

func scanEntry(row pgx.Row) (*domain.JournalEntry, error) {
	var e domain.JournalEntry
	if err := row.Scan(
		&e.ID, &e.UserID, &e.QuestionID, &e.LocalDate,
		&e.Body, &e.Source, &e.ChatSessionID, &e.CreatedAt, &e.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &e, nil
}

// EntryWithPrompt is "the question and the answer" for one row. Used by
// the daily-summary worker to compose its LLM prompt — joining at the
// store keeps the worker free of SQL.
type EntryWithPrompt struct {
	QuestionID string `json:"question_id"`
	Prompt     string `json:"prompt"`
	Body       string `json:"body"`
}

// ListByDateWithPrompts joins journal_entries to questions for one
// calendar day, returning each (question text, body) pair the user
// answered. Archived questions are still joined — historical entries
// against archived questions stay readable.
func (s *EntryStore) ListByDateWithPrompts(
	ctx context.Context,
	userID string,
	localDate time.Time,
) ([]EntryWithPrompt, error) {
	const q = `SELECT e.question_id, q.prompt, e.body
	             FROM journal_entries e
	             JOIN questions q ON q.id = e.question_id
	            WHERE e.user_id = $1 AND e.local_date = $2
	         ORDER BY q.position ASC, q.created_at ASC`
	rows, err := s.DB.Query(ctx, q, userID, localDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EntryWithPrompt, 0)
	for rows.Next() {
		var e EntryWithPrompt
		if err := rows.Scan(&e.QuestionID, &e.Prompt, &e.Body); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// HasEntryOnDate is a cheap probe used by the lazy-seed path: was today
// already the user's first-write-of-the-day for this period? If so,
// scheduling is skipped to avoid INSERT churn on every keystroke save.
func (s *EntryStore) HasEntryOnDate(ctx context.Context, userID string, localDate time.Time) (bool, error) {
	const q = `SELECT EXISTS(
	    SELECT 1 FROM journal_entries
	     WHERE user_id = $1 AND local_date = $2
	)`
	var exists bool
	if err := s.DB.QueryRow(ctx, q, userID, localDate).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// ListByDate returns all entries for one calendar day. Used by DailyEntry
// (today=`localDate`) and HistoryView (any past day).
func (s *EntryStore) ListByDate(ctx context.Context, userID string, localDate time.Time) ([]domain.JournalEntry, error) {
	const q = `SELECT ` + entryColumns + `
	  FROM journal_entries
	 WHERE user_id = $1 AND local_date = $2`
	rows, err := s.DB.Query(ctx, q, userID, localDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.JournalEntry, 0)
	for rows.Next() {
		var e domain.JournalEntry
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.QuestionID, &e.LocalDate,
			&e.Body, &e.Source, &e.ChatSessionID, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListByQuestion returns every entry the user has logged against one
// question, newest first. Used by the Summary → By Question timeline.
// Returns ErrEntryQuestionMissing when the question id isn't the
// caller's; this prevents enumeration via timing.
func (s *EntryStore) ListByQuestion(
	ctx context.Context,
	userID, questionID string,
	limit int,
) ([]domain.JournalEntry, error) {
	// Confirm the question is the caller's. Archived questions are
	// allowed — historical entries against archived questions stay
	// readable in the timeline.
	var owned int
	if err := s.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM questions WHERE id = $1 AND user_id = $2`,
		questionID, userID,
	).Scan(&owned); err != nil {
		return nil, err
	}
	if owned == 0 {
		return nil, ErrEntryQuestionMissing
	}

	q := `SELECT ` + entryColumns + `
	        FROM journal_entries
	       WHERE user_id = $1 AND question_id = $2
	    ORDER BY local_date DESC`
	args := []any{userID, questionID}
	if limit > 0 {
		q += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.JournalEntry, 0)
	for rows.Next() {
		var e domain.JournalEntry
		if err := rows.Scan(
			&e.ID, &e.UserID, &e.QuestionID, &e.LocalDate,
			&e.Body, &e.Source, &e.ChatSessionID, &e.CreatedAt, &e.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EntryDateSummary is what HistoryView lists: one row per calendar day the
// user has any entries for, plus a count so the UI can show "3 of 5
// answered" without a second query.
type EntryDateSummary struct {
	LocalDate  string `json:"local_date"`
	EntryCount int    `json:"entry_count"`
}

// ListDates returns all distinct local_dates with at least one entry,
// newest first. Bounded by `limit`; pass 0 for "no cap".
func (s *EntryStore) ListDates(ctx context.Context, userID string, limit int) ([]EntryDateSummary, error) {
	q := `SELECT to_char(local_date, 'YYYY-MM-DD') AS local_date,
	             COUNT(*)::int
	        FROM journal_entries
	       WHERE user_id = $1
	    GROUP BY local_date
	    ORDER BY local_date DESC`
	args := []any{userID}
	if limit > 0 {
		q += ` LIMIT $2`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]EntryDateSummary, 0)
	for rows.Next() {
		var d EntryDateSummary
		if err := rows.Scan(&d.LocalDate, &d.EntryCount); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// HeatmapDay is one row in the History heatmap response. `Mood` is nil when
// the user hasn't logged a mood for that day; `Answered` and `ChatTurns`
// default to 0 (NULL is squashed in SQL via COALESCE).
type HeatmapDay struct {
	LocalDate string `json:"local_date"`
	Answered  int    `json:"answered"`
	ChatTurns int    `json:"chat_turns"`
	Mood      *int   `json:"mood,omitempty"`
}

// HeatmapRange returns one row per day in [from, to] for which the user
// has any signal — at least one journal_entry, daily_inputs row, or
// chat_session. Empty days are omitted; the FE backfills level-0 cells
// from the calendar grid for visualisation.
//
// Returns rows ordered ascending by date.
func (s *EntryStore) HeatmapRange(
	ctx context.Context,
	userID string,
	from, to time.Time,
) ([]HeatmapDay, error) {
	const q = `
WITH dates AS (
  SELECT DISTINCT local_date AS d FROM journal_entries
   WHERE user_id = $1 AND local_date BETWEEN $2 AND $3
  UNION
  SELECT DISTINCT local_date AS d FROM daily_inputs
   WHERE user_id = $1 AND local_date BETWEEN $2 AND $3
  UNION
  SELECT DISTINCT local_date AS d FROM chat_sessions
   WHERE user_id = $1 AND local_date BETWEEN $2 AND $3
),
entries AS (
  SELECT local_date AS d, COUNT(*)::int AS answered
    FROM journal_entries
   WHERE user_id = $1 AND local_date BETWEEN $2 AND $3
   GROUP BY local_date
),
turns AS (
  SELECT s.local_date AS d,
         COUNT(m.id) FILTER (WHERE m.role IN ('user','assistant'))::int AS chat_turns
    FROM chat_sessions s
    LEFT JOIN chat_messages m ON m.session_id = s.id
   WHERE s.user_id = $1 AND s.local_date BETWEEN $2 AND $3
   GROUP BY s.local_date
),
inputs AS (
  SELECT local_date AS d, mood
    FROM daily_inputs
   WHERE user_id = $1 AND local_date BETWEEN $2 AND $3
)
SELECT to_char(d.d, 'YYYY-MM-DD'),
       COALESCE(e.answered, 0),
       COALESCE(t.chat_turns, 0),
       i.mood
  FROM dates d
  LEFT JOIN entries e ON e.d = d.d
  LEFT JOIN turns   t ON t.d = d.d
  LEFT JOIN inputs  i ON i.d = d.d
 ORDER BY d.d ASC`
	rows, err := s.DB.Query(ctx, q, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]HeatmapDay, 0)
	for rows.Next() {
		var hd HeatmapDay
		if err := rows.Scan(&hd.LocalDate, &hd.Answered, &hd.ChatTurns, &hd.Mood); err != nil {
			return nil, err
		}
		out = append(out, hd)
	}
	return out, rows.Err()
}

// Upsert writes (or overwrites) the entry for one (user, question, day).
// Empty body is treated as "delete the entry" so the UI can clear an
// answer by saving an empty textarea — saves us a separate DELETE
// endpoint and matches user mental model.
//
// Returns ErrEntryQuestionMissing when the question doesn't belong to the
// user or is archived; this is a server-side 404 because the FK alone
// would 500 with a less-clear message.
func (s *EntryStore) Upsert(
	ctx context.Context,
	userID, questionID string,
	localDate time.Time,
	body, source string,
) (*domain.JournalEntry, bool, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	// Confirm the question is the caller's and not archived. Without this
	// the FK lets you write entries against any user's question id you
	// happen to know — it only enforces existence.
	var owned int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM questions
		  WHERE id = $1 AND user_id = $2 AND archived_at IS NULL`,
		questionID, userID,
	).Scan(&owned); err != nil {
		return nil, false, err
	}
	if owned == 0 {
		return nil, false, ErrEntryQuestionMissing
	}

	if body == "" {
		ct, err := tx.Exec(ctx,
			`DELETE FROM journal_entries
			  WHERE user_id = $1 AND question_id = $2 AND local_date = $3`,
			userID, questionID, localDate,
		)
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return nil, ct.RowsAffected() > 0, nil
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO journal_entries (user_id, question_id, local_date, body, source)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, question_id, local_date) DO UPDATE
		    SET body = EXCLUDED.body,
		        source = EXCLUDED.source,
		        updated_at = now()
		 RETURNING `+entryColumns,
		userID, questionID, localDate, body, source,
	)
	e, err := scanEntry(row)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return e, true, nil
}

// UpsertFromChat is the chat extraction worker's writer. Overwrites
// any existing row (manual or chat) for the same (user, question, day).
// The FE warns the user before triggering extraction so they consent
// to losing prior values for the questions covered in conversation.
//
// Empty body is a silent no-op (different from Upsert's empty-deletes
// semantics). Extraction returning empty for an uncovered question
// must not delete a manual entry the user wrote.
//
// Returns (entry, true, nil) on insert/update; (nil, false, nil) when
// body was empty; (nil, false, ErrEntryQuestionMissing) for archived
// or cross-tenant question ids.
func (s *EntryStore) UpsertFromChat(
	ctx context.Context,
	userID, questionID string,
	localDate time.Time,
	body, chatSessionID string,
) (*domain.JournalEntry, bool, error) {
	if body == "" {
		return nil, false, nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	var owned int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM questions
		  WHERE id = $1 AND user_id = $2 AND archived_at IS NULL`,
		questionID, userID,
	).Scan(&owned); err != nil {
		return nil, false, err
	}
	if owned == 0 {
		return nil, false, ErrEntryQuestionMissing
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO journal_entries
		    (user_id, question_id, local_date, body, source, chat_session_id)
		 VALUES ($1, $2, $3, $4, 'chat', $5)
		 ON CONFLICT (user_id, question_id, local_date) DO UPDATE
		    SET body            = EXCLUDED.body,
		        source          = 'chat',
		        chat_session_id = EXCLUDED.chat_session_id,
		        updated_at      = now()
		 RETURNING `+entryColumns,
		userID, questionID, localDate, body, chatSessionID,
	)
	e, err := scanEntry(row)
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return e, true, nil
}

// UpsertIfAbsent (legacy) writes only when no row exists. Kept for any
// call sites that want manual-wins; the chat extraction worker now
// uses UpsertFromChat with explicit overwrite semantics + FE warning.
//
// Empty body is a silent no-op (different from Upsert's empty-deletes
// semantics). Extraction returning empty for an uncovered question
// must not delete a manual entry the user wrote in parallel.
//
// Returns (entry, true, nil) on insert; (nil, false, nil) when an
// existing row was preserved or body was empty;
// (nil, false, ErrEntryQuestionMissing) for archived/cross-tenant
// question ids — the worker logs and continues.
func (s *EntryStore) UpsertIfAbsent(
	ctx context.Context,
	userID, questionID string,
	localDate time.Time,
	body, source, chatSessionID string,
) (*domain.JournalEntry, bool, error) {
	if body == "" {
		return nil, false, nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	var owned int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM questions
		  WHERE id = $1 AND user_id = $2 AND archived_at IS NULL`,
		questionID, userID,
	).Scan(&owned); err != nil {
		return nil, false, err
	}
	if owned == 0 {
		return nil, false, ErrEntryQuestionMissing
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO journal_entries
		    (user_id, question_id, local_date, body, source, chat_session_id)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_id, question_id, local_date) DO NOTHING
		 RETURNING `+entryColumns,
		userID, questionID, localDate, body, source, chatSessionID,
	)
	e, err := scanEntry(row)
	if errors.Is(err, pgx.ErrNoRows) {
		// Existing manual entry preserved.
		if err := tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return e, true, nil
}

// UpdateBody overwrites the body of an existing entry by id, scoped to
// the caller. Empty body deletes the row, matching Upsert's contract so
// the UI can clear an answer with the same call.
//
// Returns (nil, true, nil) on a successful delete; (entry, true, nil) on
// update; (nil, false, ErrEntryNotFound) when the id isn't the caller's
// or doesn't exist.
func (s *EntryStore) UpdateBody(
	ctx context.Context,
	userID, entryID, body string,
) (*domain.JournalEntry, bool, error) {
	if body == "" {
		ct, err := s.DB.Exec(ctx,
			`DELETE FROM journal_entries
			  WHERE id = $1 AND user_id = $2`,
			entryID, userID,
		)
		if err != nil {
			return nil, false, err
		}
		if ct.RowsAffected() == 0 {
			return nil, false, ErrEntryNotFound
		}
		return nil, true, nil
	}
	row := s.DB.QueryRow(ctx,
		`UPDATE journal_entries
		    SET body = $1, updated_at = now()
		  WHERE id = $2 AND user_id = $3
		  RETURNING `+entryColumns,
		body, entryID, userID,
	)
	e, err := scanEntry(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrEntryNotFound
	}
	if err != nil {
		return nil, false, err
	}
	return e, true, nil
}
