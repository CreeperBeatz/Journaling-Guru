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
    surprise_text, step, goal_notes, new_goal_ids, chat_session_id,
    completed_at, created_at, updated_at`

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
		&wr.ChatSessionID,
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

// Delete removes the weekly_reflections row for (userID, weekStart) if
// one exists. Used by Replay to bring the user back to the "not started"
// IdleScreen. The linked chat_session row is NOT cascade-deleted —
// chat_session_id FK on weekly_reflections is one-way; the chat session
// keeps its messages so the user can pick up where they left off when
// they re-enter the Reflection tab. Returns (deleted, nil) where
// deleted is true iff a row was actually removed.
func (s *WeeklyReflectionStore) Delete(
	ctx context.Context, userID string, weekStart time.Time,
) (bool, error) {
	ct, err := s.DB.Exec(ctx,
		`DELETE FROM weekly_reflections WHERE user_id = $1 AND week_start = $2`,
		userID, weekStart,
	)
	if err != nil {
		return false, err
	}
	return ct.RowsAffected() > 0, nil
}

// Replay clears completed_at and rewinds the wizard cursor to step 1.
// Preserves surprise_text, goal_notes, and new_goal_ids — the user is
// re-walking the same reflection, not starting a fresh one. Returns
// (nil, nil) if no row exists for the (userID, weekStart) pair.
func (s *WeeklyReflectionStore) Replay(
	ctx context.Context, userID string, weekStart time.Time,
) (*domain.WeeklyReflection, error) {
	const q = `
		UPDATE weekly_reflections
		   SET completed_at = NULL,
		       step         = 1,
		       updated_at   = now()
		 WHERE user_id = $1 AND week_start = $2
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart))
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

// LatestBeforeWeek returns the most recent COMPLETED reflection with a
// week_start strictly before `weekStart`. Used by the weekly synthesis
// worker to thread last week's surprise_text into this week's prompt
// ("matching what was noticed last week…"). Returns (nil, nil) when no
// prior completed reflection exists.
func (s *WeeklyReflectionStore) LatestBeforeWeek(
	ctx context.Context, userID string, weekStart time.Time,
) (*domain.WeeklyReflection, error) {
	const q = `SELECT ` + weeklyReflectionColumns + `
	             FROM weekly_reflections
	            WHERE user_id = $1
	              AND week_start < $2
	              AND completed_at IS NOT NULL
	            ORDER BY week_start DESC
	            LIMIT 1`
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// SetChatSession writes the weekly chat_sessions.id FK onto the
// reflection row. Idempotent — re-writes the same id (the chat session is
// itself idempotent on (user, week_start)). Returns (nil, nil) when the
// reflection row doesn't exist yet.
func (s *WeeklyReflectionStore) SetChatSession(
	ctx context.Context, userID string, weekStart time.Time, sessionID string,
) (*domain.WeeklyReflection, error) {
	const q = `
		UPDATE weekly_reflections
		   SET chat_session_id = $3,
		       updated_at      = now()
		 WHERE user_id = $1 AND week_start = $2
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// MarkCompletedBySession stamps completed_at via the chat_session_id FK.
// Used by the weekly chat's finalize path which has the session id in
// hand but not necessarily the (userID, weekStart) pair.
func (s *WeeklyReflectionStore) MarkCompletedBySession(
	ctx context.Context, sessionID string,
) (*domain.WeeklyReflection, error) {
	const q = `
		UPDATE weekly_reflections
		   SET completed_at = COALESCE(completed_at, now()),
		       updated_at   = now()
		 WHERE chat_session_id = $1
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return wr, err
}

// SetSurpriseText overwrites the surprise_text column from the post-chat
// extract step. Empty input is allowed (and explicitly stored) so a
// failed extract resets continuity instead of leaving stale text.
func (s *WeeklyReflectionStore) SetSurpriseText(
	ctx context.Context, userID string, weekStart time.Time, text string,
) (*domain.WeeklyReflection, error) {
	const q = `
		UPDATE weekly_reflections
		   SET surprise_text = $3,
		       updated_at    = now()
		 WHERE user_id = $1 AND week_start = $2
		RETURNING ` + weeklyReflectionColumns
	wr, err := scanWeeklyReflection(s.DB.QueryRow(ctx, q, userID, weekStart, text))
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
