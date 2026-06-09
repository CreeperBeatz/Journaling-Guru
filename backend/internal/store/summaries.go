package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// SummaryStore reads and writes the `summaries` table. Every method is
// scoped by user_id; the UNIQUE (user_id, period_type, period_start)
// constraint is what keeps retries idempotent.
type SummaryStore struct {
	DB *pgxpool.Pool
}

func NewSummaryStore(db *pgxpool.Pool) *SummaryStore { return &SummaryStore{DB: db} }

// ErrSummaryNotFound is returned by ops that target an id that doesn't
// belong to the caller (or doesn't exist). Surfaced as 404.
var ErrSummaryNotFound = errors.New("summary not found")

const summaryColumns = `id, user_id, period_type,
    to_char(period_start, 'YYYY-MM-DD') AS period_start,
    to_char(period_end, 'YYYY-MM-DD')   AS period_end,
    body, metadata, model, prompt_tokens, completion_tokens, generated_at`

func scanSummary(row pgx.Row) (*domain.Summary, error) {
	var s domain.Summary
	var meta []byte
	if err := row.Scan(
		&s.ID, &s.UserID, &s.PeriodType,
		&s.PeriodStart, &s.PeriodEnd,
		&s.Body, &meta, &s.Model, &s.PromptTokens, &s.CompletionTokens, &s.GeneratedAt,
	); err != nil {
		return nil, err
	}
	if len(meta) > 0 {
		if err := json.Unmarshal(meta, &s.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshal metadata: %w", err)
		}
	}
	return &s, nil
}

// Upsert writes (or overwrites) a summary for one (user, period_type,
// period_start). Used both by the worker (first generation) and the
// regenerate endpoint — the ON CONFLICT branch guarantees we never end
// up with two summaries for the same period.
func (s *SummaryStore) Upsert(
	ctx context.Context,
	userID string,
	periodType string,
	periodStart, periodEnd time.Time,
	body string,
	meta domain.SummaryMetadata,
	model string,
	promptTokens, completionTokens int,
) (*domain.Summary, error) {
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}
	const q = `
		INSERT INTO summaries
		    (user_id, period_type, period_start, period_end,
		     body, metadata, model, prompt_tokens, completion_tokens)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (user_id, period_type, period_start) DO UPDATE
		   SET period_end       = EXCLUDED.period_end,
		       body             = EXCLUDED.body,
		       metadata         = EXCLUDED.metadata,
		       model            = EXCLUDED.model,
		       prompt_tokens    = EXCLUDED.prompt_tokens,
		       completion_tokens= EXCLUDED.completion_tokens,
		       generated_at     = now()
		RETURNING ` + summaryColumns
	row := s.DB.QueryRow(ctx, q,
		userID, periodType, periodStart, periodEnd,
		body, metaJSON, model, promptTokens, completionTokens,
	)
	return scanSummary(row)
}

// GetByID returns the summary scoped to userID, or ErrSummaryNotFound.
func (s *SummaryStore) GetByID(ctx context.Context, userID, id string) (*domain.Summary, error) {
	const q = `SELECT ` + summaryColumns + ` FROM summaries
	            WHERE id = $1 AND user_id = $2`
	row := s.DB.QueryRow(ctx, q, id, userID)
	out, err := scanSummary(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSummaryNotFound
	}
	return out, err
}

// GetByPeriod returns the summary for one (user, period_type,
// period_start), or nil if none exists yet. The worker uses this to check
// for prior runs before deciding to skip/regenerate; the SummaryDetail
// page uses the by-id variant instead.
func (s *SummaryStore) GetByPeriod(
	ctx context.Context,
	userID, periodType string,
	periodStart time.Time,
) (*domain.Summary, error) {
	const q = `SELECT ` + summaryColumns + ` FROM summaries
	            WHERE user_id = $1 AND period_type = $2 AND period_start = $3`
	row := s.DB.QueryRow(ctx, q, userID, periodType, periodStart)
	out, err := scanSummary(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// LatestByPeriodTypeUpTo returns the most recent summary row for one
// (user, period_type) with period_start <= asOf. Used by the reflection
// handler's synthesis lookup as a fallback when GetByPeriod's exact
// (user, period_type, period_start) match misses — e.g. legacy rows
// from before the reflection_weekday anchoring change, or when a user
// opens the wizard on a non-reflection day.
//
// Returns nil (not an error) when the user has no rows of this type
// at-or-before asOf.
func (s *SummaryStore) LatestByPeriodTypeUpTo(
	ctx context.Context,
	userID, periodType string,
	asOf time.Time,
) (*domain.Summary, error) {
	const q = `SELECT ` + summaryColumns + ` FROM summaries
	            WHERE user_id = $1
	              AND period_type = $2
	              AND period_start <= $3
	            ORDER BY period_start DESC
	            LIMIT 1`
	row := s.DB.QueryRow(ctx, q, userID, periodType, asOf)
	out, err := scanSummary(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return out, err
}

// ListByPeriodType returns the user's summaries of one period type,
// newest first. Used by the SummariesPage tabs. Limit 0 = no cap.
func (s *SummaryStore) ListByPeriodType(
	ctx context.Context,
	userID, periodType string,
	limit int,
) ([]domain.Summary, error) {
	q := `SELECT ` + summaryColumns + ` FROM summaries
	      WHERE user_id = $1 AND period_type = $2
	      ORDER BY period_start DESC`
	args := []any{userID, periodType}
	if limit > 0 {
		q += ` LIMIT $3`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Summary, 0)
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *summary)
	}
	return out, rows.Err()
}

// ListDailyInRange returns daily summaries (oldest first) for a user
// between [since, until] inclusive. The worker uses this to assemble the
// weekly/monthly/yearly prompt context (we feed in lower-level summaries,
// not raw entries — keeps the prompt token-bounded).
func (s *SummaryStore) ListDailyInRange(
	ctx context.Context,
	userID string,
	since, until time.Time,
) ([]domain.Summary, error) {
	return s.listInRange(ctx, userID, string(domain.PeriodDay), since, until)
}

// ListInRange is the generic version: any period_type. Used to assemble
// monthly prompts (from weekly summaries) and yearly (from monthly).
func (s *SummaryStore) ListInRange(
	ctx context.Context,
	userID, periodType string,
	since, until time.Time,
) ([]domain.Summary, error) {
	return s.listInRange(ctx, userID, periodType, since, until)
}

// ListOverlappingRange returns summaries of `periodType` whose
// [period_start, period_end] overlaps [from, to], oldest first. The
// monthly synthesis uses this to collect the month's weekly letters:
// weeks straddle month boundaries (the final week of June can end on
// July 5), so a period_start-only filter would drop the edges.
func (s *SummaryStore) ListOverlappingRange(
	ctx context.Context,
	userID, periodType string,
	from, to time.Time,
) ([]domain.Summary, error) {
	const q = `SELECT ` + summaryColumns + ` FROM summaries
	           WHERE user_id = $1 AND period_type = $2
	             AND period_start <= $4 AND period_end >= $3
	        ORDER BY period_start ASC`
	rows, err := s.DB.Query(ctx, q, userID, periodType, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Summary, 0)
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *summary)
	}
	return out, rows.Err()
}

func (s *SummaryStore) listInRange(
	ctx context.Context,
	userID, periodType string,
	since, until time.Time,
) ([]domain.Summary, error) {
	const q = `SELECT ` + summaryColumns + ` FROM summaries
	           WHERE user_id = $1 AND period_type = $2
	             AND period_start >= $3 AND period_start <= $4
	        ORDER BY period_start ASC`
	rows, err := s.DB.Query(ctx, q, userID, periodType, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Summary, 0)
	for rows.Next() {
		summary, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *summary)
	}
	return out, rows.Err()
}

// MoodPoint is one (date, score) pair for the SummariesPage sparkline.
// We pull these from daily summaries' metadata.mood_score so the chart
// doesn't have to scan the full body.
type MoodPoint struct {
	LocalDate string  `json:"local_date"`
	Score     float64 `json:"score"`
}

// MoodSeries returns daily mood scores for a user across the last N days,
// oldest first. Days without a daily summary or without a mood_score in
// metadata are skipped — the frontend renders a discontinuous line.
func (s *SummaryStore) MoodSeries(ctx context.Context, userID string, days int) ([]MoodPoint, error) {
	const q = `
		SELECT to_char(period_start, 'YYYY-MM-DD') AS local_date,
		       (metadata->>'mood_score')::float8   AS score
		  FROM summaries
		 WHERE user_id = $1
		   AND period_type = 'day'
		   AND metadata ? 'mood_score'
		   AND period_start >= (current_date - $2::int)
		ORDER BY period_start ASC`
	rows, err := s.DB.Query(ctx, q, userID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MoodPoint, 0)
	for rows.Next() {
		var p MoodPoint
		if err := rows.Scan(&p.LocalDate, &p.Score); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EmotionCount is one emotion-frequency bucket for the stats panel.
type EmotionCount struct {
	Emotion string `json:"emotion"`
	Count   int    `json:"count"`
}

// TopEmotions returns the most-frequent emotions across the user's daily
// summaries in the last `days` days. Limit caps the result; pass 0 for "no
// cap". jsonb_array_elements_text un-nests the metadata.emotions array.
func (s *SummaryStore) TopEmotions(
	ctx context.Context,
	userID string,
	days, limit int,
) ([]EmotionCount, error) {
	q := `
		SELECT lower(emotion) AS emotion, COUNT(*)::int AS count
		  FROM summaries,
		       jsonb_array_elements_text(metadata->'emotions') AS emotion
		 WHERE user_id = $1
		   AND period_type = 'day'
		   AND period_start >= (current_date - $2::int)
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
	out := make([]EmotionCount, 0)
	for rows.Next() {
		var e EmotionCount
		if err := rows.Scan(&e.Emotion, &e.Count); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
