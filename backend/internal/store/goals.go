package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// GoalStore reads and writes the per-user goal list. Goal IDs are stable
// across status transitions; ending a goal flips status + outcome but
// keeps every check-in and the title intact for the Zone-3 ledger.
type GoalStore struct {
	DB *pgxpool.Pool
}

func NewGoalStore(db *pgxpool.Pool) *GoalStore { return &GoalStore{DB: db} }

const goalColumns = `id, user_id, title, check_in_question,
    to_char(start_date, 'YYYY-MM-DD') AS start_date,
    to_char(end_date,   'YYYY-MM-DD') AS end_date,
    status, outcome, conclusion_text, created_at, ended_at`

func scanGoal(row pgx.Row) (*domain.Goal, error) {
	var g domain.Goal
	if err := row.Scan(
		&g.ID, &g.UserID, &g.Title, &g.CheckInQuestion,
		&g.StartDate, &g.EndDate,
		&g.Status, &g.Outcome, &g.ConclusionText,
		&g.CreatedAt, &g.EndedAt,
	); err != nil {
		return nil, err
	}
	return &g, nil
}

// Create inserts a new active goal. The SMART shaper has already
// produced a measurable check_in_question by the time this is called;
// this method does not validate that further.
func (s *GoalStore) Create(
	ctx context.Context,
	userID, title, checkInQuestion string,
	startDate, endDate time.Time,
) (*domain.Goal, error) {
	const q = `
		INSERT INTO goals (user_id, title, check_in_question, start_date, end_date)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + goalColumns
	return scanGoal(s.DB.QueryRow(ctx, q, userID, title, checkInQuestion, startDate, endDate))
}

// GetByID returns the goal scoped to userID, or nil if not found / wrong tenant.
func (s *GoalStore) GetByID(ctx context.Context, userID, id string) (*domain.Goal, error) {
	const q = `SELECT ` + goalColumns + `
	             FROM goals
	            WHERE id = $1 AND user_id = $2`
	g, err := scanGoal(s.DB.QueryRow(ctx, q, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// ListActive returns every status='active' goal for the user, ordered by
// end_date ascending so the daily flow surfaces near-end goals first.
// `asOf` filters out goals whose end_date is already past — callers
// should kick those into the wrap-up flow before showing them.
func (s *GoalStore) ListActive(
	ctx context.Context, userID string, asOf time.Time,
) ([]domain.Goal, error) {
	const q = `SELECT ` + goalColumns + `
	             FROM goals
	            WHERE user_id = $1 AND status = 'active'
	              AND end_date >= $2
	            ORDER BY end_date ASC, created_at ASC`
	rows, err := s.DB.Query(ctx, q, userID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Goal, 0)
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// ListAll returns every goal (active + historical) ordered newest-first.
// Drives Zone 3 of the summary page.
func (s *GoalStore) ListAll(ctx context.Context, userID string) ([]domain.Goal, error) {
	const q = `SELECT ` + goalColumns + `
	             FROM goals
	            WHERE user_id = $1
	            ORDER BY created_at DESC`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Goal, 0)
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// Extend pushes an active goal's end_date forward by `addDays` days.
// Stays status='active'. Returns nil row if the goal isn't active or
// belongs to another user. Used by the weekly reflection card —
// callers compute `addDays` via timezone.NextReflectionWeekday so the
// new end_date still lands on a reflection_weekday.
func (s *GoalStore) Extend(
	ctx context.Context, userID, id string, addDays int,
) (*domain.Goal, error) {
	const q = `
		UPDATE goals
		   SET end_date = end_date + make_interval(days => $3)
		 WHERE id = $1 AND user_id = $2 AND status = 'active'
		RETURNING ` + goalColumns
	g, err := scanGoal(s.DB.QueryRow(ctx, q, id, userID, addDays))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// EnumDueWrapUps returns every active goal whose end_date <= asOf — the
// daily lifecycle worker uses this to surface wrap-up prompts. Each
// returned goal is still status='active' (Complete/Abandon hasn't been
// called yet); the worker is expected to nag rather than auto-close.
func (s *GoalStore) EnumDueWrapUps(
	ctx context.Context, userID string, asOf time.Time,
) ([]domain.Goal, error) {
	const q = `SELECT ` + goalColumns + `
	             FROM goals
	            WHERE user_id = $1 AND status = 'active' AND end_date <= $2
	            ORDER BY end_date ASC`
	rows, err := s.DB.Query(ctx, q, userID, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Goal, 0)
	for rows.Next() {
		g, err := scanGoal(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *g)
	}
	return out, rows.Err()
}

// Complete transitions an active goal to status='completed' with an
// outcome (kept | dropped | inconclusive) and an optional conclusion.
// Idempotent against re-submission of the same outcome — the CHECK on
// goals enforces "active iff (outcome IS NULL AND ended_at IS NULL)" so
// re-completing with a different outcome only succeeds via this method.
func (s *GoalStore) Complete(
	ctx context.Context, userID, id, outcome, conclusionText string,
) (*domain.Goal, error) {
	const q = `
		UPDATE goals
		   SET status = 'completed',
		       outcome = $3,
		       conclusion_text = $4,
		       ended_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'active'
		RETURNING ` + goalColumns
	g, err := scanGoal(s.DB.QueryRow(ctx, q, id, userID, outcome, conclusionText))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// Abandon transitions an active goal to status='abandoned'. Conclusion
// text is the optional "what didn't work?" answer. Outcome is set to
// 'dropped' so Zone-3 reads don't have to special-case the status.
func (s *GoalStore) Abandon(
	ctx context.Context, userID, id, conclusionText string,
) (*domain.Goal, error) {
	const q = `
		UPDATE goals
		   SET status = 'abandoned',
		       outcome = 'dropped',
		       conclusion_text = $3,
		       ended_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'active'
		RETURNING ` + goalColumns
	g, err := scanGoal(s.DB.QueryRow(ctx, q, id, userID, conclusionText))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}
