package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
    mood_score, emotions_text, classified_emotions, notes, created_at, updated_at`

func scanDailyInput(row pgx.Row) (*domain.DailyInput, error) {
	var d domain.DailyInput
	var classified []byte
	if err := row.Scan(
		&d.ID, &d.UserID, &d.LocalDate,
		&d.MoodScore, &d.EmotionsText, &classified, &d.Notes,
		&d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	d.ClassifiedEmotions = []domain.ClassifiedEmotion{}
	if len(classified) > 0 {
		if err := json.Unmarshal(classified, &d.ClassifiedEmotions); err != nil {
			return nil, fmt.Errorf("unmarshal classified_emotions: %w", err)
		}
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

// Upsert writes (or overwrites) the row for one (user, local_date).
// If all three user-controlled fields would be empty (mood nil,
// emotions_text blank, notes blank), the row is *deleted* — keeps the
// table free of empty rows and matches the "empty body deletes" pattern
// from journal_entries.
//
// classified_emotions is owned by the EmotionClassifyWorker — handler
// writes never touch it. On INSERT it stays at the column default '[]';
// on UPDATE we deliberately omit it from SET so a re-save preserves the
// previous classification until the worker re-runs.
//
// Returns (input, true, nil) on insert/update; (nil, true, nil) on
// delete-because-empty; (input, false, nil) on no-op (no existing row
// and nothing to write).
func (s *DailyInputStore) Upsert(
	ctx context.Context,
	userID string,
	localDate time.Time,
	mood *int,
	emotionsText string,
	notes string,
) (*domain.DailyInput, bool, error) {
	emotionsText = strings.TrimSpace(emotionsText)
	allEmpty := mood == nil && emotionsText == "" && notes == ""
	if allEmpty {
		ct, err := s.DB.Exec(ctx,
			`DELETE FROM daily_inputs WHERE user_id = $1 AND local_date = $2`,
			userID, localDate)
		if err != nil {
			return nil, false, err
		}
		return nil, ct.RowsAffected() > 0, nil
	}
	const q = `
		INSERT INTO daily_inputs (user_id, local_date, mood_score, emotions_text, notes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET mood_score    = EXCLUDED.mood_score,
		       emotions_text = EXCLUDED.emotions_text,
		       notes         = EXCLUDED.notes,
		       updated_at    = now()
		RETURNING ` + dailyInputColumns
	row := s.DB.QueryRow(ctx, q, userID, localDate, mood, emotionsText, notes)
	out, err := scanDailyInput(row)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// OverwriteFromExtraction is the chat extraction writer. Overwrites
// mood/emotions/notes from extraction output — the user is warned at
// the FE before clicking "Update check-in", so they consent to losing
// any prior manual values for those fields.
//
// Empty extracted values DO NOT blank existing fields. If chat covered
// mood but not notes, manual notes are preserved. classified_emotions
// is owned by the EmotionClassifyWorker and is never touched here; the
// caller schedules a re-classify after writing emotions_text so the
// classifier output catches up to the new text.
func (s *DailyInputStore) OverwriteFromExtraction(
	ctx context.Context,
	userID string,
	localDate time.Time,
	mood *int,
	emotions string,
	notes string,
) (*domain.DailyInput, error) {
	emotions = strings.TrimSpace(emotions)
	notes = strings.TrimSpace(notes)
	if mood == nil && emotions == "" && notes == "" {
		return s.GetByDate(ctx, userID, localDate)
	}
	const q = `
		INSERT INTO daily_inputs (user_id, local_date, mood_score, emotions_text, notes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET mood_score    = COALESCE(EXCLUDED.mood_score, daily_inputs.mood_score),
		       emotions_text = CASE WHEN EXCLUDED.emotions_text = ''
		                            THEN daily_inputs.emotions_text
		                            ELSE EXCLUDED.emotions_text
		                       END,
		       notes         = CASE WHEN EXCLUDED.notes = ''
		                            THEN daily_inputs.notes
		                            ELSE EXCLUDED.notes
		                       END,
		       updated_at    = now()
		RETURNING ` + dailyInputColumns
	row := s.DB.QueryRow(ctx, q, userID, localDate, mood, emotions, notes)
	out, err := scanDailyInput(row)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// MergeFromExtraction (legacy) — left in place for backwards-compat
// but no longer the default chat extraction writer. Was used to give
// "manual-wins per field"; the new chat UX explicitly warns the user
// before overwriting, so OverwriteFromExtraction is preferred.
func (s *DailyInputStore) MergeFromExtraction(
	ctx context.Context,
	userID string,
	localDate time.Time,
	mood *int,
	emotions string,
	notes string,
) (*domain.DailyInput, error) {
	emotions = strings.TrimSpace(emotions)
	notes = strings.TrimSpace(notes)
	allEmpty := mood == nil && emotions == "" && notes == ""
	if allEmpty {
		return s.GetByDate(ctx, userID, localDate)
	}
	const q = `
		INSERT INTO daily_inputs (user_id, local_date, mood_score, emotions_text, notes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET mood_score    = COALESCE(daily_inputs.mood_score, EXCLUDED.mood_score),
		       emotions_text = CASE WHEN daily_inputs.emotions_text = ''
		                            THEN EXCLUDED.emotions_text
		                            ELSE daily_inputs.emotions_text
		                       END,
		       notes         = CASE WHEN daily_inputs.notes = ''
		                            THEN EXCLUDED.notes
		                            ELSE daily_inputs.notes
		                       END,
		       updated_at    = now()
		RETURNING ` + dailyInputColumns
	row := s.DB.QueryRow(ctx, q, userID, localDate, mood, emotions, notes)
	out, err := scanDailyInput(row)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WriteClassifiedEmotions overwrites the classifier output column. Called
// by the EmotionClassifyWorker on completion (or to clear the column to
// `[]` when emotions_text becomes empty). No-op if the row is gone (the
// worker raced a delete; harmless).
func (s *DailyInputStore) WriteClassifiedEmotions(
	ctx context.Context,
	userID string,
	localDate time.Time,
	classified []domain.ClassifiedEmotion,
) error {
	if classified == nil {
		classified = []domain.ClassifiedEmotion{}
	}
	buf, err := json.Marshal(classified)
	if err != nil {
		return fmt.Errorf("marshal classified_emotions: %w", err)
	}
	_, err = s.DB.Exec(ctx,
		`UPDATE daily_inputs
		    SET classified_emotions = $3,
		        updated_at = now()
		  WHERE user_id = $1 AND local_date = $2`,
		userID, localDate, buf)
	return err
}

// MoodPoint mirrors the existing summaries.MoodSeries shape so the
// stats panel's chart code stays the same after the source-of-truth
// switch.
type DailyMoodPoint struct {
	LocalDate string  `json:"local_date"`
	Score     float64 `json:"score"`
}

// MoodSeries returns daily mood scores over the last `days` days, oldest
// first. Days where mood_score IS NULL are skipped (the chart renders
// a discontinuous line).
func (s *DailyInputStore) MoodSeries(
	ctx context.Context, userID string, days int,
) ([]DailyMoodPoint, error) {
	const q = `
		SELECT to_char(local_date, 'YYYY-MM-DD') AS local_date,
		       mood_score::float8                AS score
		  FROM daily_inputs
		 WHERE user_id = $1
		   AND mood_score IS NOT NULL
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

// EmotionCount mirrors summaries.EmotionCount so the stats endpoint can
// keep returning the same shape. The string is now a Plutchik subtype
// ("ecstasy", "annoyance") rather than a free-form chip word.
type DailyEmotionCount struct {
	Emotion string `json:"emotion"`
	Count   int    `json:"count"`
}

// TopEmotions returns the most-frequent classified subtypes across
// daily_inputs in the last `days` days. Limit caps the result; pass 0
// for "no cap". Reads classified_emotions[*].subtype — rows where the
// classifier hasn't run yet (or returned no entries) contribute nothing.
func (s *DailyInputStore) TopEmotions(
	ctx context.Context, userID string, days, limit int,
) ([]DailyEmotionCount, error) {
	q := `
		SELECT lower(e->>'subtype') AS emotion, COUNT(*)::int AS count
		  FROM daily_inputs,
		       jsonb_array_elements(classified_emotions) AS e
		 WHERE user_id = $1
		   AND local_date >= (current_date - $2::int)
		   AND e ? 'subtype'
		GROUP BY lower(e->>'subtype')
		ORDER BY count DESC, emotion ASC`
	args := []any{userID, days}
	if limit > 0 {
		q += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DailyEmotionCount, 0)
	for rows.Next() {
		var e DailyEmotionCount
		if err := rows.Scan(&e.Emotion, &e.Count); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// AggregatedMetadata bundles the pieces the worker needs to compose
// summaries.metadata for a higher-level period (week/month/year):
// weighted-average mood, top-N emotions by frequency, and the day
// count. Computed in SQL so we don't haul N rows into Go just to count.
type AggregatedMetadata struct {
	MoodScore  *float64 `json:"mood_score"`
	Emotions   []string `json:"emotions"`
	EntryCount int      `json:"entry_count"`
}

// AggregateForRange computes the aggregated metadata over all
// daily_inputs rows in [since, until] inclusive for the user.
//
// Mood: arithmetic mean of non-null mood_scores. Empty range → MoodScore=nil.
// Emotions: top `topN` Plutchik subtypes by occurrence count across the
// classified_emotions arrays in range.
// EntryCount: number of distinct local_dates with any input in range.
func (s *DailyInputStore) AggregateForRange(
	ctx context.Context, userID string, since, until time.Time, topN int,
) (*AggregatedMetadata, error) {
	out := &AggregatedMetadata{Emotions: []string{}}

	const moodQuery = `
		SELECT AVG(mood_score)::float8, COUNT(*)::int
		  FROM daily_inputs
		 WHERE user_id = $1
		   AND local_date BETWEEN $2 AND $3`
	var moodAvg *float64
	if err := s.DB.QueryRow(ctx, moodQuery, userID, since, until).Scan(&moodAvg, &out.EntryCount); err != nil {
		return nil, err
	}
	out.MoodScore = moodAvg

	emoQuery := `
		SELECT lower(e->>'subtype') AS emotion, COUNT(*)::int AS count
		  FROM daily_inputs,
		       jsonb_array_elements(classified_emotions) AS e
		 WHERE user_id = $1
		   AND local_date BETWEEN $2 AND $3
		   AND e ? 'subtype'
		GROUP BY lower(e->>'subtype')
		ORDER BY count DESC, emotion ASC`
	args := []any{userID, since, until}
	if topN > 0 {
		emoQuery += ` LIMIT $4`
		args = append(args, topN)
	}
	rows, err := s.DB.Query(ctx, emoQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e string
		var count int
		if err := rows.Scan(&e, &count); err != nil {
			return nil, err
		}
		out.Emotions = append(out.Emotions, e)
	}
	return out, rows.Err()
}

// HasContentInRange reports whether the user has any daily_inputs row
// (with mood, emotions_text, or notes) in [since, until]. Used by the
// daily worker's skip check — a "just notes" or "just mood" day still
// warrants a summary. Raw text counts as content; whether classification
// has finished doesn't gate the daily summary.
func (s *DailyInputStore) HasContentInRange(
	ctx context.Context, userID string, since, until time.Time,
) (bool, error) {
	const q = `SELECT EXISTS(
	    SELECT 1 FROM daily_inputs
	     WHERE user_id = $1
	       AND local_date BETWEEN $2 AND $3
	       AND (mood_score IS NOT NULL
	            OR emotions_text <> ''
	            OR notes <> '')
	)`
	var exists bool
	if err := s.DB.QueryRow(ctx, q, userID, since, until).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}
