package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// MonthlyReflectionStore reads and writes the per-(user, month_start)
// monthly reflection state. Idempotency anchor is UNIQUE (user_id,
// month_start) — Start is an upsert so the lazy-create on the reflection
// GET and the worker's belt-and-braces create never race.
type MonthlyReflectionStore struct {
	DB *pgxpool.Pool
}

func NewMonthlyReflectionStore(db *pgxpool.Pool) *MonthlyReflectionStore {
	return &MonthlyReflectionStore{DB: db}
}

const monthlyReflectionColumns = `id, user_id,
    to_char(month_start, 'YYYY-MM-DD') AS month_start,
    to_char(month_end,   'YYYY-MM-DD') AS month_end,
    to_char(week_start,  'YYYY-MM-DD') AS week_start,
    chat_session_id, direction_text, intention_text, intention_set_at,
    ratings, ratings_set_at, completed_at, created_at, updated_at`

func scanMonthlyReflection(row pgx.Row) (*domain.MonthlyReflection, error) {
	var (
		mr          domain.MonthlyReflection
		ratingsJSON []byte
	)
	if err := row.Scan(
		&mr.ID, &mr.UserID,
		&mr.MonthStart, &mr.MonthEnd, &mr.WeekStart,
		&mr.ChatSessionID, &mr.DirectionText, &mr.IntentionText, &mr.IntentionSetAt,
		&ratingsJSON, &mr.RatingsSetAt, &mr.CompletedAt, &mr.CreatedAt, &mr.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if len(ratingsJSON) > 0 {
		_ = json.Unmarshal(ratingsJSON, &mr.Ratings)
	}
	return &mr, nil
}

// Start lazily creates a row for (userID, monthStart) and (re-)anchors
// the hosting week_start — on carry-over the user does the monthly a week
// late, and the row should point at the week that actually hosted it.
// Idempotent; returns the row in either case.
func (s *MonthlyReflectionStore) Start(
	ctx context.Context, userID string, monthStart, monthEnd, weekStart time.Time,
) (*domain.MonthlyReflection, error) {
	const q = `
		INSERT INTO monthly_reflections (user_id, month_start, month_end, week_start)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, month_start) DO UPDATE
		    SET week_start = EXCLUDED.week_start,
		        updated_at = now()
		RETURNING ` + monthlyReflectionColumns
	return scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart, monthEnd, weekStart))
}

// GetByMonthStart returns the row for (userID, monthStart) or (nil, nil)
// when the month has never been started.
func (s *MonthlyReflectionStore) GetByMonthStart(
	ctx context.Context, userID string, monthStart time.Time,
) (*domain.MonthlyReflection, error) {
	const q = `SELECT ` + monthlyReflectionColumns + `
	             FROM monthly_reflections
	            WHERE user_id = $1 AND month_start = $2`
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// GetByWeekStart returns the row whose hosting week is weekStart, or
// (nil, nil). Used by the history view, which is keyed by week.
func (s *MonthlyReflectionStore) GetByWeekStart(
	ctx context.Context, userID string, weekStart time.Time,
) (*domain.MonthlyReflection, error) {
	const q = `SELECT ` + monthlyReflectionColumns + `
	             FROM monthly_reflections
	            WHERE user_id = $1 AND week_start = $2`
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, weekStart))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// SetChatSession writes the combined chat_sessions.id FK onto the row.
// Idempotent. Returns (nil, nil) when the row doesn't exist yet.
func (s *MonthlyReflectionStore) SetChatSession(
	ctx context.Context, userID string, monthStart time.Time, sessionID string,
) (*domain.MonthlyReflection, error) {
	const q = `
		UPDATE monthly_reflections
		   SET chat_session_id = $3,
		       updated_at      = now()
		 WHERE user_id = $1 AND month_start = $2
		RETURNING ` + monthlyReflectionColumns
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// SetIntention overwrites intention_text and stamps intention_set_at.
// Called when the user accepts (or edits) a propose_intention card, and
// by the extraction fallback when the chat ended without an accepted
// card. Returns (nil, nil) when no row exists.
func (s *MonthlyReflectionStore) SetIntention(
	ctx context.Context, userID string, monthStart time.Time, text string,
) (*domain.MonthlyReflection, error) {
	const q = `
		UPDATE monthly_reflections
		   SET intention_text   = $3,
		       intention_set_at = now(),
		       updated_at       = now()
		 WHERE user_id = $1 AND month_start = $2
		RETURNING ` + monthlyReflectionColumns
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart, text))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// SetRatings overwrites the life check-in jsonb and stamps
// ratings_set_at. Validation (known keys, 0..10) happens at the handler
// via domain.ValidateRatings. Returns (nil, nil) when no row exists.
func (s *MonthlyReflectionStore) SetRatings(
	ctx context.Context, userID string, monthStart time.Time, ratings map[string]int,
) (*domain.MonthlyReflection, error) {
	payload, err := json.Marshal(ratings)
	if err != nil {
		return nil, err
	}
	const q = `
		UPDATE monthly_reflections
		   SET ratings        = $3,
		       ratings_set_at = now(),
		       updated_at     = now()
		 WHERE user_id = $1 AND month_start = $2
		RETURNING ` + monthlyReflectionColumns
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart, payload))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// SetDirection overwrites direction_text from the post-chat extract.
// Empty input is allowed (a failed extract resets rather than leaving
// stale text — same convention as the weekly surprise extract).
func (s *MonthlyReflectionStore) SetDirection(
	ctx context.Context, userID string, monthStart time.Time, text string,
) (*domain.MonthlyReflection, error) {
	const q = `
		UPDATE monthly_reflections
		   SET direction_text = $3,
		       updated_at     = now()
		 WHERE user_id = $1 AND month_start = $2
		RETURNING ` + monthlyReflectionColumns
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart, text))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// MarkCompleted stamps completed_at if not already set.
func (s *MonthlyReflectionStore) MarkCompleted(
	ctx context.Context, userID string, monthStart time.Time,
) (*domain.MonthlyReflection, error) {
	const q = `
		UPDATE monthly_reflections
		   SET completed_at = COALESCE(completed_at, now()),
		       updated_at   = now()
		 WHERE user_id = $1 AND month_start = $2
		RETURNING ` + monthlyReflectionColumns
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// MarkCompletedBySession stamps completed_at via the chat_session_id FK.
// Used by the combined chat's finalize paths (sync handler and idle
// sweeper) which hold the session id, not the month anchor.
func (s *MonthlyReflectionStore) MarkCompletedBySession(
	ctx context.Context, sessionID string,
) (*domain.MonthlyReflection, error) {
	const q = `
		UPDATE monthly_reflections
		   SET completed_at = COALESCE(completed_at, now()),
		       updated_at   = now()
		 WHERE chat_session_id = $1
		RETURNING ` + monthlyReflectionColumns
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// ReplayClear rewinds the month for a replayed hosting week: clears
// completed_at and direction_text but PRESERVES intention_text and
// ratings — those are user artifacts (like the kept chat transcript),
// not derived state. Returns (nil, nil) when no row exists.
func (s *MonthlyReflectionStore) ReplayClear(
	ctx context.Context, userID string, monthStart time.Time,
) (*domain.MonthlyReflection, error) {
	const q = `
		UPDATE monthly_reflections
		   SET completed_at   = NULL,
		       direction_text = '',
		       updated_at     = now()
		 WHERE user_id = $1 AND month_start = $2
		RETURNING ` + monthlyReflectionColumns
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, monthStart))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// LatestBefore returns the most recent COMPLETED monthly reflection with
// month_start strictly before `before`. Feeds last month's intention +
// direction into the next monthly synthesis and (lightly) into weekly
// chat context during the following month. Returns (nil, nil) when none
// exists.
func (s *MonthlyReflectionStore) LatestBefore(
	ctx context.Context, userID string, before time.Time,
) (*domain.MonthlyReflection, error) {
	const q = `SELECT ` + monthlyReflectionColumns + `
	             FROM monthly_reflections
	            WHERE user_id = $1
	              AND month_start < $2
	              AND completed_at IS NOT NULL
	            ORDER BY month_start DESC
	            LIMIT 1`
	mr, err := scanMonthlyReflection(s.DB.QueryRow(ctx, q, userID, before))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return mr, err
}

// ListRatingsInRange returns rows with non-NULL ratings whose month_start
// is in [from, to], oldest first. Feeds the ratings trend into the
// monthly letter (prior months only — never the month being synthesized)
// and, later, the yearly chart.
func (s *MonthlyReflectionStore) ListRatingsInRange(
	ctx context.Context, userID string, from, to time.Time,
) ([]domain.MonthlyReflection, error) {
	const q = `SELECT ` + monthlyReflectionColumns + `
	             FROM monthly_reflections
	            WHERE user_id = $1
	              AND month_start >= $2 AND month_start <= $3
	              AND ratings IS NOT NULL
	            ORDER BY month_start ASC`
	rows, err := s.DB.Query(ctx, q, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.MonthlyReflection, 0)
	for rows.Next() {
		mr, err := scanMonthlyReflection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *mr)
	}
	return out, rows.Err()
}
