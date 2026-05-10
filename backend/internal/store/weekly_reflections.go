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

// WeeklyReflectionStore reads and writes the per-(user, week_start)
// wizard state. Idempotency anchor is UNIQUE (user_id, week_start) — the
// /start endpoint relies on ON CONFLICT DO NOTHING so a double-tap on
// the start button is a no-op.
type WeeklyReflectionStore struct {
	DB *pgxpool.Pool
}

func NewWeeklyReflectionStore(db *pgxpool.Pool) *WeeklyReflectionStore {
	return &WeeklyReflectionStore{DB: db}
}

const weeklyReflectionColumns = `id, user_id,
    to_char(week_start, 'YYYY-MM-DD') AS week_start,
    to_char(week_end,   'YYYY-MM-DD') AS week_end,
    surprise_text, step, goal_notes, new_goal_ids, completed_at, created_at, updated_at`

func scanWeeklyReflection(row pgx.Row) (*domain.WeeklyReflection, error) {
	var (
		wr           domain.WeeklyReflection
		notesJSON    []byte
		newGoalsJSON []byte
	)
	if err := row.Scan(
		&wr.ID, &wr.UserID,
		&wr.WeekStart, &wr.WeekEnd,
		&wr.SurpriseText, &wr.Step, &notesJSON, &newGoalsJSON,
		&wr.CompletedAt, &wr.CreatedAt, &wr.UpdatedAt,
	); err != nil {
		return nil, err
	}
	wr.GoalNotes = map[string]string{}
	if len(notesJSON) > 0 {
		_ = json.Unmarshal(notesJSON, &wr.GoalNotes)
	}
	wr.NewGoalIDs = []string{}
	if len(newGoalsJSON) > 0 {
		_ = json.Unmarshal(newGoalsJSON, &wr.NewGoalIDs)
	}
	return &wr, nil
}

// AddNewGoalID appends a goal_id to new_goal_ids if not already present.
// Used by the wizard's Card 3 after a commit_goal save lands so the
// Done page can split active_goals into "Active" vs "New".
func (s *WeeklyReflectionStore) AddNewGoalID(
	ctx context.Context, userID string, weekStart time.Time, goalID string,
) (*domain.WeeklyReflection, error) {
	const q = `
		UPDATE weekly_reflections
		   SET new_goal_ids = CASE
		           WHEN new_goal_ids @> to_jsonb($3::text)
		               THEN new_goal_ids
		           ELSE new_goal_ids || to_jsonb($3::text)
		       END,
		       updated_at = now()
		 WHERE user_id = $1 AND week_start = $2
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart, goalID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// Start lazily creates a row for (userID, weekStart). Returns the row in
// either case — fresh insert or pre-existing. Idempotent.
func (s *WeeklyReflectionStore) Start(
	ctx context.Context, userID string, weekStart, weekEnd time.Time,
) (*domain.WeeklyReflection, error) {
	const q = `
		INSERT INTO weekly_reflections (user_id, week_start, week_end)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, week_start) DO UPDATE
		    SET updated_at = weekly_reflections.updated_at
		RETURNING ` + weeklyReflectionColumns
	return scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart, weekEnd))
}

// GetByWeekStart returns the row for (userID, weekStart) or (nil, nil)
// when no /start has happened yet for that week.
func (s *WeeklyReflectionStore) GetByWeekStart(
	ctx context.Context, userID string, weekStart time.Time,
) (*domain.WeeklyReflection, error) {
	const q = `SELECT ` + weeklyReflectionColumns + `
	             FROM weekly_reflections
	            WHERE user_id = $1 AND week_start = $2`
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// WeeklyReflectionPatch is a partial update. Pointer fields = "leave
// alone if nil"; goal note merge happens via SetGoalNote.
type WeeklyReflectionPatch struct {
	SurpriseText *string
	Step         *int
}

// Patch applies a partial update for (userID, weekStart). Returns the
// updated row or (nil, nil) if no row exists. Use Start first.
func (s *WeeklyReflectionStore) Patch(
	ctx context.Context, userID string, weekStart time.Time, patch WeeklyReflectionPatch,
) (*domain.WeeklyReflection, error) {
	const q = `
		UPDATE weekly_reflections
		   SET surprise_text = COALESCE($3, surprise_text),
		       step          = COALESCE($4, step),
		       updated_at    = now()
		 WHERE user_id = $1 AND week_start = $2
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q,
		userID, weekStart, patch.SurpriseText, patch.Step))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// SetGoalNote merges a single goal_id → text entry into the JSONB
// goal_notes column. Empty text removes the key. Returns the updated row.
func (s *WeeklyReflectionStore) SetGoalNote(
	ctx context.Context, userID string, weekStart time.Time, goalID, text string,
) (*domain.WeeklyReflection, error) {
	if text == "" {
		const qDel = `
			UPDATE weekly_reflections
			   SET goal_notes = goal_notes - $3,
			       updated_at = now()
			 WHERE user_id = $1 AND week_start = $2
			RETURNING ` + weeklyReflectionColumns
		wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, qDel, userID, weekStart, goalID))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return wr, err
	}
	const qSet = `
		UPDATE weekly_reflections
		   SET goal_notes = goal_notes || jsonb_build_object($3::text, $4::text),
		       updated_at = now()
		 WHERE user_id = $1 AND week_start = $2
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, qSet, userID, weekStart, goalID, text))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// MarkCompleted sets completed_at = now() if not already set. Returns
// the row in either case.
func (s *WeeklyReflectionStore) MarkCompleted(
	ctx context.Context, userID string, weekStart time.Time,
) (*domain.WeeklyReflection, error) {
	const q = `
		UPDATE weekly_reflections
		   SET completed_at = COALESCE(completed_at, now()),
		       updated_at   = now()
		 WHERE user_id = $1 AND week_start = $2
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// ListInRange returns every weekly_reflection whose week_start is in
// [from, to]. Used by the heatmap endpoint to flag which days have a
// completed weekly reflection so the FE can paint a badge. Includes
// in-progress and completed rows alike — the FE shows the badge only
// for completed_at IS NOT NULL.
func (s *WeeklyReflectionStore) ListInRange(
	ctx context.Context, userID string, from, to time.Time,
) ([]domain.WeeklyReflection, error) {
	const q = `SELECT ` + weeklyReflectionColumns + `
	             FROM weekly_reflections
	            WHERE user_id = $1
	              AND week_start >= $2 AND week_start <= $3
	            ORDER BY week_start ASC`
	rows, err := s.DB.Query(ctx, q, userID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.WeeklyReflection, 0)
	for rows.Next() {
		wr, err := scanWeeklyReflection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wr)
	}
	return out, rows.Err()
}
