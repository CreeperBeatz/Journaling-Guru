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
    mood_score, emotions, notes, created_at, updated_at`

func scanDailyInput(row pgx.Row) (*domain.DailyInput, error) {
	var d domain.DailyInput
	var emotions []byte
	if err := row.Scan(
		&d.ID, &d.UserID, &d.LocalDate,
		&d.MoodScore, &emotions, &d.Notes,
		&d.CreatedAt, &d.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(emotions) == 0 {
		d.Emotions = []string{}
	} else if err := json.Unmarshal(emotions, &d.Emotions); err != nil {
		return nil, fmt.Errorf("unmarshal emotions: %w", err)
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
// If all three fields would be empty (mood nil, emotions empty, notes
// blank), the row is *deleted* — keeps the table free of empty rows
// and matches the "empty body deletes" pattern from journal_entries.
//
// Returns (input, true, nil) on insert/update; (nil, true, nil) on
// delete-because-empty; (input, false, nil) on no-op (no existing row
// and nothing to write).
func (s *DailyInputStore) Upsert(
	ctx context.Context,
	userID string,
	localDate time.Time,
	mood *int,
	emotions []string,
	notes string,
) (*domain.DailyInput, bool, error) {
	emotions = normalizeEmotions(emotions)
	emotionsJSON, err := json.Marshal(emotions)
	if err != nil {
		return nil, false, fmt.Errorf("marshal emotions: %w", err)
	}
	allEmpty := mood == nil && len(emotions) == 0 && notes == ""
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
		INSERT INTO daily_inputs (user_id, local_date, mood_score, emotions, notes)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET mood_score = EXCLUDED.mood_score,
		       emotions   = EXCLUDED.emotions,
		       notes      = EXCLUDED.notes,
		       updated_at = now()
		RETURNING ` + dailyInputColumns
	row := s.DB.QueryRow(ctx, q, userID, localDate, mood, emotionsJSON, notes)
	out, err := scanDailyInput(row)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
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
// keep returning the same shape.
type DailyEmotionCount struct {
	Emotion string `json:"emotion"`
	Count   int    `json:"count"`
}

// TopEmotions returns the most-frequent emotions across daily_inputs in
// the last `days` days. Limit caps the result; pass 0 for "no cap".
func (s *DailyInputStore) TopEmotions(
	ctx context.Context, userID string, days, limit int,
) ([]DailyEmotionCount, error) {
	q := `
		SELECT lower(emotion) AS emotion, COUNT(*)::int AS count
		  FROM daily_inputs,
		       jsonb_array_elements_text(emotions) AS emotion
		 WHERE user_id = $1
		   AND local_date >= (current_date - $2::int)
		GROUP BY lower(emotion)
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
// Mood: arithmetic mean of non-null mood_scores (we don't have a
// natural per-day weight; entries-per-day was a proxy under the old
// design but daily_inputs is already per-day). Empty range → MoodScore=nil.
//
// Emotions: top `topN` lower-case emotions by occurrence count.
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
		SELECT lower(emotion) AS emotion, COUNT(*)::int AS count
		  FROM daily_inputs,
		       jsonb_array_elements_text(emotions) AS emotion
		 WHERE user_id = $1
		   AND local_date BETWEEN $2 AND $3
		GROUP BY lower(emotion)
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
// (with mood, emotions, or notes) in [since, until]. Used by the daily
// worker's skip check — a "just notes + mood" day still warrants a
// summary.
func (s *DailyInputStore) HasContentInRange(
	ctx context.Context, userID string, since, until time.Time,
) (bool, error) {
	const q = `SELECT EXISTS(
	    SELECT 1 FROM daily_inputs
	     WHERE user_id = $1
	       AND local_date BETWEEN $2 AND $3
	       AND (mood_score IS NOT NULL
	            OR jsonb_array_length(emotions) > 0
	            OR notes <> '')
	)`
	var exists bool
	if err := s.DB.QueryRow(ctx, q, userID, since, until).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// normalizeEmotions trims, lowercases, dedupes, and clips to a
// reasonable cap. The frontend already does this on save, but
// belt-and-suspenders — the worker reads these straight into the
// summary metadata and we don't want stray casing to fork the count.
func normalizeEmotions(in []string) []string {
	const maxLen = 8
	const maxStringLen = 32
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		if len(v) > maxStringLen {
			v = v[:maxStringLen]
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
		if len(out) >= maxLen {
			break
		}
	}
	return out
}
