package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// DailyInputStore reads and writes the per-day check-in. Every method is
// scoped by user_id; the UNIQUE (user_id, local_date) constraint keeps
// the upsert idempotent.
type DailyInputStore struct {
	DB *pgxpool.Pool
}

func NewDailyInputStore(db *pgxpool.Pool) *DailyInputStore { return &DailyInputStore{DB: db} }

const dailyInputColumns = `id, user_id,
    to_char(local_date, 'YYYY-MM-DD') AS local_date,
    mood, drained_text, charged_text, gratitude_text, reflection_text,
    backfilled, edited_at, created_at, updated_at`

func scanDailyInput(row pgx.Row) (*domain.DailyInput, error) {
	var d domain.DailyInput
	if err := row.Scan(
		&d.ID, &d.UserID, &d.LocalDate,
		&d.Mood,
		&d.DrainedText, &d.ChargedText, &d.GratitudeText, &d.ReflectionText,
		&d.Backfilled, &d.EditedAt,
		&d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &d, nil
}

// GetByDate returns the row for one user/day, or nil if the user
// hasn't logged anything for that day yet. The handler distinguishes
// 404 (wrong-tenant id) from 200-empty (no input yet).
func (s *DailyInputStore) GetByDate(
	ctx context.Context, userID string, localDate time.Time,
) (*domain.DailyInput, error) {
	const q = `SELECT ` + dailyInputColumns + `
	             FROM daily_inputs
	            WHERE user_id = $1 AND local_date = $2`
	row := s.DB.QueryRow(ctx, q, userID, localDate)
	out, err := scanDailyInput(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// DailyInputUpsert is the bag of fields the handler passes for a manual
// save. Pointer/string semantics:
//
//   - Mood nil  → unset (delete the row when all-empty, otherwise set
//     mood to NULL on conflict).
//   - text fields: trimmed before write. Empty string is a *legitimate*
//     value for "user cleared the field"; under Upsert it's treated as
//     "not set yet" only when *every* field is empty (then we delete).
//
// Backfilled is set by the handler when the local_date being written is
// older than today's user_local_date.
type DailyInputUpsert struct {
	Mood           *int
	DrainedText    string
	ChargedText    string
	GratitudeText  string
	ReflectionText string
	Backfilled     bool
}

func (u DailyInputUpsert) allEmpty() bool {
	return u.Mood == nil &&
		strings.TrimSpace(u.DrainedText) == "" &&
		strings.TrimSpace(u.ChargedText) == "" &&
		strings.TrimSpace(u.GratitudeText) == "" &&
		strings.TrimSpace(u.ReflectionText) == ""
}

// Upsert writes (or overwrites) the row for one (user, local_date).
// If every user-controlled field would be empty, the row is *deleted* —
// keeps the table free of empty rows and matches the "empty body
// deletes" pattern from journal_entries.
//
// Returns (input, true, nil) on insert/update; (nil, true, nil) on
// delete-because-empty; (input, false, nil) on no-op (no existing row
// and nothing to write).
func (s *DailyInputStore) Upsert(
	ctx context.Context,
	userID string,
	localDate time.Time,
	in DailyInputUpsert,
) (*domain.DailyInput, bool, error) {
	in.DrainedText = strings.TrimSpace(in.DrainedText)
	in.ChargedText = strings.TrimSpace(in.ChargedText)
	in.GratitudeText = strings.TrimSpace(in.GratitudeText)
	in.ReflectionText = strings.TrimSpace(in.ReflectionText)
	if in.allEmpty() {
		ct, err := s.DB.Exec(ctx,
			`DELETE FROM daily_inputs WHERE user_id = $1 AND local_date = $2`,
			userID, localDate)
		if err != nil {
			return nil, false, err
		}
		return nil, ct.RowsAffected() > 0, nil
	}
	const q = `
		INSERT INTO daily_inputs
		    (user_id, local_date, mood, drained_text, charged_text,
		     gratitude_text, reflection_text, backfilled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET mood            = EXCLUDED.mood,
		       drained_text    = EXCLUDED.drained_text,
		       charged_text    = EXCLUDED.charged_text,
		       gratitude_text  = EXCLUDED.gratitude_text,
		       reflection_text = EXCLUDED.reflection_text,
		       backfilled      = EXCLUDED.backfilled OR daily_inputs.backfilled,
		       edited_at       = CASE WHEN daily_inputs.created_at < now() - interval '5 seconds'
		                              THEN now()
		                              ELSE daily_inputs.edited_at
		                         END,
		       updated_at      = now()
		RETURNING ` + dailyInputColumns
	row := s.DB.QueryRow(ctx, q, userID, localDate,
		in.Mood, in.DrainedText, in.ChargedText, in.GratitudeText, in.ReflectionText, in.Backfilled)
	out, err := scanDailyInput(row)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// MergeFromExtraction is the chat extraction writer with manual-wins
// semantics: existing user values survive, extracted values fill gaps.
//
//   - Mood: COALESCE(existing, extracted) — manual wins if set.
//   - Each text field: existing wins if non-empty; otherwise extracted.
//
// Empty extraction values DO NOT blank existing fields. This is the
// "manual edit during extraction window is never clobbered" guarantee
// referenced in CLAUDE.md.
//
// Tag attachments live in daily_entry_tags and are written by the
// extraction worker via DailyEntryTagStore.ReplaceForDay — they are
// additive to whatever the user manually picked, NOT routed through
// this method.
func (s *DailyInputStore) MergeFromExtraction(
	ctx context.Context,
	userID string,
	localDate time.Time,
	in DailyInputUpsert,
) (*domain.DailyInput, error) {
	in.DrainedText = strings.TrimSpace(in.DrainedText)
	in.ChargedText = strings.TrimSpace(in.ChargedText)
	in.GratitudeText = strings.TrimSpace(in.GratitudeText)
	in.ReflectionText = strings.TrimSpace(in.ReflectionText)
	if in.allEmpty() {
		return s.GetByDate(ctx, userID, localDate)
	}
	const q = `
		INSERT INTO daily_inputs
		    (user_id, local_date, mood, drained_text, charged_text,
		     gratitude_text, reflection_text, backfilled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, false)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET mood            = COALESCE(daily_inputs.mood, EXCLUDED.mood),
		       drained_text    = CASE WHEN daily_inputs.drained_text = ''
		                              THEN EXCLUDED.drained_text
		                              ELSE daily_inputs.drained_text END,
		       charged_text    = CASE WHEN daily_inputs.charged_text = ''
		                              THEN EXCLUDED.charged_text
		                              ELSE daily_inputs.charged_text END,
		       gratitude_text  = CASE WHEN daily_inputs.gratitude_text = ''
		                              THEN EXCLUDED.gratitude_text
		                              ELSE daily_inputs.gratitude_text END,
		       reflection_text = CASE WHEN daily_inputs.reflection_text = ''
		                              THEN EXCLUDED.reflection_text
		                              ELSE daily_inputs.reflection_text END,
		       updated_at      = now()
		RETURNING ` + dailyInputColumns
	row := s.DB.QueryRow(ctx, q, userID, localDate,
		in.Mood, in.DrainedText, in.ChargedText, in.GratitudeText, in.ReflectionText)
	out, err := scanDailyInput(row)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// OverwriteFromExtraction is the session-wins counterpart to
// MergeFromExtraction. Used when the user explicitly chooses "Finish &
// replace from this session" — mood + each text field is replaced with
// what the session produced.
//
// Empty/NULL safety: an empty extracted string does NOT blank an
// existing field. The motivation is "session-wins where the session
// said something" — nothing said means leave it alone, so the user
// can manually edit a field the chat didn't cover and still keep it.
// Mood: COALESCE(extracted, existing) — if the LLM emitted null mood
// (ambiguous tone) we keep what the user had.
func (s *DailyInputStore) OverwriteFromExtraction(
	ctx context.Context,
	userID string,
	localDate time.Time,
	in DailyInputUpsert,
) (*domain.DailyInput, error) {
	in.DrainedText = strings.TrimSpace(in.DrainedText)
	in.ChargedText = strings.TrimSpace(in.ChargedText)
	in.GratitudeText = strings.TrimSpace(in.GratitudeText)
	in.ReflectionText = strings.TrimSpace(in.ReflectionText)
	if in.allEmpty() {
		return s.GetByDate(ctx, userID, localDate)
	}
	const q = `
		INSERT INTO daily_inputs
		    (user_id, local_date, mood, drained_text, charged_text,
		     gratitude_text, reflection_text, backfilled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, false)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET mood            = COALESCE(EXCLUDED.mood, daily_inputs.mood),
		       drained_text    = CASE WHEN EXCLUDED.drained_text = ''
		                              THEN daily_inputs.drained_text
		                              ELSE EXCLUDED.drained_text END,
		       charged_text    = CASE WHEN EXCLUDED.charged_text = ''
		                              THEN daily_inputs.charged_text
		                              ELSE EXCLUDED.charged_text END,
		       gratitude_text  = CASE WHEN EXCLUDED.gratitude_text = ''
		                              THEN daily_inputs.gratitude_text
		                              ELSE EXCLUDED.gratitude_text END,
		       reflection_text = CASE WHEN EXCLUDED.reflection_text = ''
		                              THEN daily_inputs.reflection_text
		                              ELSE EXCLUDED.reflection_text END,
		       updated_at      = now()
		RETURNING ` + dailyInputColumns
	row := s.DB.QueryRow(ctx, q, userID, localDate,
		in.Mood, in.DrainedText, in.ChargedText, in.GratitudeText, in.ReflectionText)
	out, err := scanDailyInput(row)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DailyMoodPoint is one (date, mood) pair on the 30-day sparkline.
type DailyMoodPoint struct {
	LocalDate string  `json:"local_date"`
	Score     float64 `json:"score"`
}

// MoodSeries returns daily mood values over the last `days` days, oldest
// first. Days where mood IS NULL are skipped (the chart renders a
// discontinuous line). The mood scale is 1..3 — Score is float64 to
// share the existing chart rendering pipeline that expects float points.
func (s *DailyInputStore) MoodSeries(
	ctx context.Context, userID string, days int,
) ([]DailyMoodPoint, error) {
	const q = `
		SELECT to_char(local_date, 'YYYY-MM-DD') AS local_date,
		       mood::float8                       AS score
		  FROM daily_inputs
		 WHERE user_id = $1
		   AND mood IS NOT NULL
		   AND local_date >= (current_date - $2::int)
		ORDER BY local_date ASC`
	rows, err := s.DB.Query(ctx, q, userID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DailyMoodPoint, 0)
	for rows.Next() {
		var p DailyMoodPoint
		if err := rows.Scan(&p.LocalDate, &p.Score); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MoodSeriesInRange is MoodSeries anchored to an explicit [since, until]
// date window (inclusive) instead of current_date. Used by the weekly
// synthesis worker so a late-firing job still reads its own period's
// moods rather than "the last N days from now".
func (s *DailyInputStore) MoodSeriesInRange(
	ctx context.Context, userID string, since, until time.Time,
) ([]DailyMoodPoint, error) {
	const q = `
		SELECT to_char(local_date, 'YYYY-MM-DD') AS local_date,
		       mood::float8                       AS score
		  FROM daily_inputs
		 WHERE user_id = $1
		   AND mood IS NOT NULL
		   AND local_date BETWEEN $2 AND $3
		ORDER BY local_date ASC`
	rows, err := s.DB.Query(ctx, q, userID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DailyMoodPoint, 0)
	for rows.Next() {
		var p DailyMoodPoint
		if err := rows.Scan(&p.LocalDate, &p.Score); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// AggregatedMetadata is the minimal shape kept after the Energy Audit
// pivot. Mood is the arithmetic mean of non-null mood values in range,
// EntryCount is the number of distinct local_dates with any input.
// Topics/Emotions are no longer tracked here — drainer/charger tags
// live in daily_entry_tags and are queried via TagStore + DailyEntryTagStore.
type AggregatedMetadata struct {
	MoodScore  *float64 `json:"mood_score"`
	EntryCount int      `json:"entry_count"`
}

// AggregateForRange computes mean mood + entry count across daily_inputs
// in [since, until] inclusive. Used by the surviving weekly summary
// worker (Zone-1 headline insight).
func (s *DailyInputStore) AggregateForRange(
	ctx context.Context, userID string, since, until time.Time,
) (*AggregatedMetadata, error) {
	const q = `
		SELECT AVG(mood)::float8, COUNT(*)::int
		  FROM daily_inputs
		 WHERE user_id = $1
		   AND local_date BETWEEN $2 AND $3`
	out := &AggregatedMetadata{}
	if err := s.DB.QueryRow(ctx, q, userID, since, until).Scan(&out.MoodScore, &out.EntryCount); err != nil {
		return nil, err
	}
	return out, nil
}

// GratitudeRow is one (date, text) pair for a non-empty gratitude entry
// in a date range. Used by the weekly synthesis worker to feed gratitude
// items into the LLM prompt without round-tripping per-day.
type GratitudeRow struct {
	LocalDate string
	Text      string
}

// ListGratitudeInRange returns gratitude_text entries in [since, until]
// (inclusive) ordered by local_date ASC. Empty strings are filtered out
// server-side.
func (s *DailyInputStore) ListGratitudeInRange(
	ctx context.Context, userID string, since, until time.Time,
) ([]GratitudeRow, error) {
	const q = `
		SELECT to_char(local_date, 'YYYY-MM-DD'), gratitude_text
		  FROM daily_inputs
		 WHERE user_id = $1
		   AND local_date BETWEEN $2 AND $3
		   AND gratitude_text <> ''
		 ORDER BY local_date ASC`
	rows, err := s.DB.Query(ctx, q, userID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]GratitudeRow, 0)
	for rows.Next() {
		var g GratitudeRow
		if err := rows.Scan(&g.LocalDate, &g.Text); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// DailyNoteRow is one (date, reflection_text) pair surfaced to the
// weekly synthesis prompt. `reflection_text` is the renamed legacy
// "notes" column — the free-text "additional notes" field on the
// per-day check-in.
type DailyNoteRow struct {
	LocalDate string
	Text      string
}

// ListNotesInRange returns non-empty reflection_text entries in
// [since, until] (inclusive) ordered by local_date ASC. Empty strings
// are filtered server-side.
func (s *DailyInputStore) ListNotesInRange(
	ctx context.Context, userID string, since, until time.Time,
) ([]DailyNoteRow, error) {
	const q = `
		SELECT to_char(local_date, 'YYYY-MM-DD'), reflection_text
		  FROM daily_inputs
		 WHERE user_id = $1
		   AND local_date BETWEEN $2 AND $3
		   AND reflection_text <> ''
		 ORDER BY local_date ASC`
	rows, err := s.DB.Query(ctx, q, userID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DailyNoteRow, 0)
	for rows.Next() {
		var n DailyNoteRow
		if err := rows.Scan(&n.LocalDate, &n.Text); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// HasContentInRange reports whether the user has any non-empty
// daily_inputs row in [since, until]. A "just mood" or "just gratitude"
// day still counts.
func (s *DailyInputStore) HasContentInRange(
	ctx context.Context, userID string, since, until time.Time,
) (bool, error) {
	const q = `SELECT EXISTS(
	    SELECT 1 FROM daily_inputs
	     WHERE user_id = $1
	       AND local_date BETWEEN $2 AND $3
	       AND (mood IS NOT NULL
	            OR drained_text <> ''
	            OR charged_text <> ''
	            OR gratitude_text <> ''
	            OR reflection_text <> '')
	)`
	var exists bool
	if err := s.DB.QueryRow(ctx, q, userID, since, until).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
