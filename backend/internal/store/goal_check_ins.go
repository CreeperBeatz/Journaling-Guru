package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// GoalCheckInStore handles the per-day yes/no answer to each active
// goal's check_in_question. (goal_id, local_date) is the PK so re-saving
// the day overwrites in place.
type GoalCheckInStore struct {
	DB *pgxpool.Pool
}

func NewGoalCheckInStore(db *pgxpool.Pool) *GoalCheckInStore {
	return &GoalCheckInStore{DB: db}
}

// Upsert sets the value for (goal, local_date). The goal is *not*
// re-validated against its date range or status here — the handler must
// reject check-ins for completed/abandoned goals or out-of-range dates.
func (s *GoalCheckInStore) Upsert(
	ctx context.Context, goalID string, localDate time.Time, value bool,
) (*domain.GoalCheckIn, error) {
	const q = `
		INSERT INTO goal_check_ins (goal_id, local_date, value)
		VALUES ($1, $2, $3)
		ON CONFLICT (goal_id, local_date) DO UPDATE
		   SET value = EXCLUDED.value,
		       updated_at = now()
		RETURNING goal_id, to_char(local_date, 'YYYY-MM-DD'), value, created_at, updated_at`
	var c domain.GoalCheckIn
	err := s.DB.QueryRow(ctx, q, goalID, localDate, value).Scan(
		&c.GoalID, &c.LocalDate, &c.Value, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// GetForDay returns every (active or historical) goal's check-in for one
// day, keyed by goal_id. Days where the user hasn't answered have no
// entry. Caller pairs this with GoalStore.ListActive to know what to
// render.
func (s *GoalCheckInStore) GetForDay(
	ctx context.Context, userID string, localDate time.Time,
) (map[string]bool, error) {
	const q = `
		SELECT gc.goal_id, gc.value
		  FROM goal_check_ins gc
		  JOIN goals g ON g.id = gc.goal_id
		 WHERE g.user_id = $1 AND gc.local_date = $2`
	rows, err := s.DB.Query(ctx, q, userID, localDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var goalID string
		var value bool
		if err := rows.Scan(&goalID, &value); err != nil {
			return nil, err
		}
		out[goalID] = value
	}
	return out, rows.Err()
}

// CountKept returns how many of [since, until] inclusive the goal was
// answered "yes". Drives the "5/7 days" tally on the weekly reflection
// view and Zone-1 active-goal status.
func (s *GoalCheckInStore) CountKept(
	ctx context.Context, goalID string, since, until time.Time,
) (kept, total int, err error) {
	const q = `
		SELECT COALESCE(SUM(CASE WHEN value THEN 1 ELSE 0 END), 0)::int,
		       COUNT(*)::int
		  FROM goal_check_ins
		 WHERE goal_id = $1 AND local_date BETWEEN $2 AND $3`
	err = s.DB.QueryRow(ctx, q, goalID, since, until).Scan(&kept, &total)
	return
}
