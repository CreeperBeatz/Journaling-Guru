package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DailyEntryTagStore is the link table between a (user, local_date) and
// the drainer/charger tags attached to that day. Composite PK
// (user_id, local_date, tag_id, role) keeps writes idempotent and lets
// chat extraction safely re-run.
type DailyEntryTagStore struct {
	DB *pgxpool.Pool
}

func NewDailyEntryTagStore(db *pgxpool.Pool) *DailyEntryTagStore {
	return &DailyEntryTagStore{DB: db}
}

// ReplaceForDay rewrites every link of the given role for one day.
// Idempotent: the chat extraction worker calls this after upserting the
// tags it extracted, and the second-time-through sees identical rows.
//
// We delete-then-insert in a transaction so a day's drainers (or
// chargers) atomically reflect the latest extraction, even if the LLM
// output shrunk between runs. The other-role rows are untouched.
func (s *DailyEntryTagStore) ReplaceForDay(
	ctx context.Context,
	userID string,
	localDate time.Time,
	role string,
	tagIDs []string,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM daily_entry_tags
		   WHERE user_id = $1 AND local_date = $2 AND role = $3`,
		userID, localDate, role,
	); err != nil {
		return err
	}
	if len(tagIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			INSERT INTO daily_entry_tags (user_id, local_date, tag_id, role)
			SELECT $1, $2, tag_id, $3
			  FROM unnest($4::uuid[]) AS tag_id`,
			userID, localDate, role, tagIDs,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Link adds a single (day, tag, role) row. Manual-tab path: user picks
// a tag from the picker → handler calls Link. ON CONFLICT DO NOTHING so
// double-clicks don't error.
func (s *DailyEntryTagStore) Link(
	ctx context.Context,
	userID string,
	localDate time.Time,
	tagID string,
	role string,
) error {
	_, err := s.DB.Exec(ctx, `
		INSERT INTO daily_entry_tags (user_id, local_date, tag_id, role)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`,
		userID, localDate, tagID, role,
	)
	return err
}

// Unlink removes a single (day, tag, role) row.
func (s *DailyEntryTagStore) Unlink(
	ctx context.Context,
	userID string,
	localDate time.Time,
	tagID string,
	role string,
) error {
	_, err := s.DB.Exec(ctx,
		`DELETE FROM daily_entry_tags
		   WHERE user_id = $1 AND local_date = $2 AND tag_id = $3 AND role = $4`,
		userID, localDate, tagID, role,
	)
	return err
}

// TagDayLink is one row in the day's tag list, used to render the
// drainer/charger pills back on /today and history.
type TagDayLink struct {
	TagID string `json:"tag_id"`
	Label string `json:"label"`
	Role  string `json:"role"`
}

// ListByDate returns every tag link for one user/day, ordered by role
// then label. Filters out merged/archived tags so renamed-and-merged
// history still resolves to the merge target's label (caller can resolve
// merged_into_tag_id explicitly if it wants the moved row).
func (s *DailyEntryTagStore) ListByDate(
	ctx context.Context, userID string, localDate time.Time,
) ([]TagDayLink, error) {
	const q = `
		SELECT t.id, t.label, det.role
		  FROM daily_entry_tags det
		  JOIN tags t ON t.id = det.tag_id
		 WHERE det.user_id = $1 AND det.local_date = $2
		   AND t.status = 'active'
		 ORDER BY det.role ASC, t.label ASC`
	rows, err := s.DB.Query(ctx, q, userID, localDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TagDayLink, 0)
	for rows.Next() {
		var l TagDayLink
		if err := rows.Scan(&l.TagID, &l.Label, &l.Role); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// TagAggregate is the Zone-2 row shape: tag + appearance count + average
// mood on the days the tag appeared. Spec calls out the "low confidence"
// flag for <7 appearances; the FE renders that, this struct just carries
// the count.
type TagAggregate struct {
	TagID       string   `json:"tag_id"`
	Label       string   `json:"label"`
	Appearances int      `json:"appearances"`
	AvgMood     *float64 `json:"avg_mood"`
}

// TopByValence returns the most-frequent active tags of one role across
// `daysBack` days for a user, with average mood (1-3) on the days each
// tag appeared. NULL avg_mood when none of the days had a recorded mood.
//
// Limit caps the result; pass 0 for "no cap". Drives Zone 2 of the
// summary page and the weekly reflection pattern view.
func (s *DailyEntryTagStore) TopByValence(
	ctx context.Context, userID, role string, daysBack, limit int,
) ([]TagAggregate, error) {
	q := `
		SELECT t.id,
		       t.label,
		       COUNT(*)::int                            AS appearances,
		       AVG(di.mood)::float8                     AS avg_mood
		  FROM daily_entry_tags det
		  JOIN tags t ON t.id = det.tag_id
		  LEFT JOIN daily_inputs di
		         ON di.user_id = det.user_id
		        AND di.local_date = det.local_date
		 WHERE det.user_id = $1
		   AND det.role = $2
		   AND t.status = 'active'
		   AND det.local_date >= (current_date - $3::int)
		 GROUP BY t.id, t.label
		 ORDER BY appearances DESC, t.label ASC`
	args := []any{userID, role, daysBack}
	if limit > 0 {
		q += ` LIMIT $4`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TagAggregate, 0)
	for rows.Next() {
		var a TagAggregate
		if err := rows.Scan(&a.TagID, &a.Label, &a.Appearances, &a.AvgMood); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// TopByValenceInRange is TopByValence anchored to an explicit
// [since, until] date window (inclusive) instead of current_date. Used
// by the weekly synthesis worker so a job that fires late still
// aggregates the tags of its own period, not "the last N days from
// whenever the worker got to it".
func (s *DailyEntryTagStore) TopByValenceInRange(
	ctx context.Context, userID, role string, since, until time.Time, limit int,
) ([]TagAggregate, error) {
	q := `
		SELECT t.id,
		       t.label,
		       COUNT(*)::int                            AS appearances,
		       AVG(di.mood)::float8                     AS avg_mood
		  FROM daily_entry_tags det
		  JOIN tags t ON t.id = det.tag_id
		  LEFT JOIN daily_inputs di
		         ON di.user_id = det.user_id
		        AND di.local_date = det.local_date
		 WHERE det.user_id = $1
		   AND det.role = $2
		   AND t.status = 'active'
		   AND det.local_date BETWEEN $3 AND $4
		 GROUP BY t.id, t.label
		 ORDER BY appearances DESC, t.label ASC`
	args := []any{userID, role, since, until}
	if limit > 0 {
		q += ` LIMIT $5`
		args = append(args, limit)
	}
	rows, err := s.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TagAggregate, 0)
	for rows.Next() {
		var a TagAggregate
		if err := rows.Scan(&a.TagID, &a.Label, &a.Appearances, &a.AvgMood); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
