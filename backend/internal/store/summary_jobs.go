package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// SummaryJobStore writes the scheduler queue. The lifecycle is documented
// in 0004_summaries.sql; here we just expose the four state transitions
// the worker and dispatcher need.
type SummaryJobStore struct {
	DB *pgxpool.Pool
}

func NewSummaryJobStore(db *pgxpool.Pool) *SummaryJobStore { return &SummaryJobStore{DB: db} }

// ErrSummaryJobNotFound is returned when a target id isn't owned by the
// caller (or doesn't exist).
var ErrSummaryJobNotFound = errors.New("summary job not found")

const summaryJobColumns = `id, user_id, period_type,
    to_char(period_start, 'YYYY-MM-DD') AS period_start,
    fire_at, fired_at, status, attempts, last_error, created_at, updated_at`

func scanSummaryJob(row pgx.Row) (*domain.SummaryJob, error) {
	var j domain.SummaryJob
	if err := row.Scan(
		&j.ID, &j.UserID, &j.PeriodType, &j.PeriodStart,
		&j.FireAt, &j.FiredAt, &j.Status, &j.Attempts, &j.LastError,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &j, nil
}

// ReArm flips a terminal summary_jobs row back to 'pending' so the
// dispatcher will re-run it. Used by the reflection handler when a
// historical week is opened without synthesis fields populated — we
// want the worker to regenerate the row's summary on the next tick.
//
// Returns whether a row was actually re-armed. false means no matching
// (user, period_type, period_start) row exists or its status isn't
// terminal — callers use this to decide whether to fall through to
// Schedule a fresh row instead of silently no-op'ing.
func (s *SummaryJobStore) ReArm(
	ctx context.Context,
	userID, periodType string,
	periodStart, fireAt time.Time,
) (bool, error) {
	const q = `
		UPDATE summary_jobs
		   SET status='pending',
		       fire_at=$4,
		       last_error=NULL,
		       updated_at=now()
		 WHERE user_id=$1
		   AND period_type=$2
		   AND period_start=$3
		   AND status IN ('completed','skipped','failed')
		RETURNING id`
	var id string
	err := s.DB.QueryRow(ctx, q, userID, periodType, periodStart, fireAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// Schedule inserts a summary_jobs row. ON CONFLICT DO NOTHING — repeated
// lazy-seed calls or cross-tab races can't double-schedule the same
// (user, period_type, period_start). Returns whether a new row was
// inserted (true) or an existing one was preserved (false).
func (s *SummaryJobStore) Schedule(
	ctx context.Context,
	userID, periodType string,
	periodStart time.Time,
	fireAt time.Time,
) (bool, error) {
	const q = `
		INSERT INTO summary_jobs (user_id, period_type, period_start, fire_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, period_type, period_start) DO NOTHING
		RETURNING id`
	var id string
	err := s.DB.QueryRow(ctx, q, userID, periodType, periodStart, fireAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ClaimedJob is the dispatcher's view of a row it just claimed. Just enough
// to enqueue a River job — the worker reads the full row by id.
type ClaimedJob struct {
	ID          string
	UserID      string
	PeriodType  string
	PeriodStart string
}

// ClaimDue atomically marks up to `limit` due rows as 'claimed' and bumps
// their attempts counter. Uses FOR UPDATE SKIP LOCKED so two dispatcher
// ticks running concurrently (e.g. during a worker rolling-restart) don't
// double-enqueue.
//
// Returns the rows in fire-time order so backlogs drain oldest-first.
func (s *SummaryJobStore) ClaimDue(ctx context.Context, limit int) ([]ClaimedJob, error) {
	const q = `
		UPDATE summary_jobs
		   SET status='claimed',
		       attempts = attempts + 1,
		       updated_at = now()
		 WHERE id IN (
		     SELECT id FROM summary_jobs
		      WHERE status='pending' AND fire_at <= now()
		      ORDER BY fire_at ASC
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, user_id, period_type,
		          to_char(period_start, 'YYYY-MM-DD') AS period_start`
	rows, err := s.DB.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ClaimedJob, 0)
	for rows.Next() {
		var c ClaimedJob
		if err := rows.Scan(&c.ID, &c.UserID, &c.PeriodType, &c.PeriodStart); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetByID loads a job for the worker. No user scoping — the worker is
// trusted code, and the job_id was just produced by the dispatcher.
func (s *SummaryJobStore) GetByID(ctx context.Context, id string) (*domain.SummaryJob, error) {
	const q = `SELECT ` + summaryJobColumns + ` FROM summary_jobs WHERE id = $1`
	row := s.DB.QueryRow(ctx, q, id)
	out, err := scanSummaryJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSummaryJobNotFound
	}
	return out, err
}

// MarkCompleted is the success path: summary written, schedule the next
// period (caller's job, not ours).
func (s *SummaryJobStore) MarkCompleted(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE summary_jobs
		    SET status='completed', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// MarkSkipped: period had no entries, so we didn't call the LLM.
func (s *SummaryJobStore) MarkSkipped(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE summary_jobs
		    SET status='skipped', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// ReleaseForRetry reverts a claimed job to pending so the dispatcher will
// re-claim it next tick. Used when River retries the worker — the job
// stays in the queue. last_error preserves the diagnostic.
func (s *SummaryJobStore) ReleaseForRetry(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE summary_jobs
		    SET status='pending', last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}

// MarkFailed is the terminal failure path: River exhausted retries. The
// scheduler will not auto-arm the next period for this period_type.
func (s *SummaryJobStore) MarkFailed(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE summary_jobs
		    SET status='failed', fired_at=now(), last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}

// ResetForRegeneration handles the "Regenerate" button: insert a fresh
// pending row, or revive any existing one by resetting status, fire_at,
// and attempts. The previous gate excluded rows already 'pending' or
// 'claimed', but that left genuinely stuck rows (worker crashed mid-
// flight, River job lost across a deploy) unrecoverable from the UI.
// Now the reset is unconditional — double-clicks are absorbed by the
// idempotent UPDATE, and the dispatcher's atomic claim guards against
// double-processing.
func (s *SummaryJobStore) ResetForRegeneration(
	ctx context.Context,
	userID, periodType string,
	periodStart, fireAt time.Time,
) (bool, error) {
	const q = `
		INSERT INTO summary_jobs (user_id, period_type, period_start, fire_at, status, attempts)
		VALUES ($1, $2, $3, $4, 'pending', 0)
		ON CONFLICT (user_id, period_type, period_start) DO UPDATE
		   SET fire_at    = EXCLUDED.fire_at,
		       status     = 'pending',
		       attempts   = 0,
		       last_error = NULL,
		       updated_at = now()
		RETURNING id`
	var id string
	err := s.DB.QueryRow(ctx, q, userID, periodType, periodStart, fireAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ReclaimStale flips 'claimed' rows whose worker side appears stuck —
// updated_at older than staleAfter — back to 'pending' so the
// dispatcher will pick them up on the next tick. Used to recover from
// worker crashes or lost River jobs after a deploy: the dispatcher
// previously claimed the row (status→claimed, updated_at=now()) and
// then handed off to River, which then failed to actually run the job,
// leaving the summary_jobs row orphaned.
//
// Bounded by attempts < maxAttempts so a job that consistently stalls
// past several reclaims is left alone — the existing River retry path
// will eventually MarkFailed it via the worker.
//
// Returns the number of rows reclaimed.
func (s *SummaryJobStore) ReclaimStale(
	ctx context.Context, staleAfter time.Duration, maxAttempts int,
) (int, error) {
	secs := int(staleAfter.Seconds())
	if secs < 1 {
		secs = 1
	}
	const q = `
		UPDATE summary_jobs
		   SET status='pending',
		       updated_at=now()
		 WHERE status='claimed'
		   AND updated_at < now() - make_interval(secs => $1)
		   AND attempts < $2`
	tag, err := s.DB.Exec(ctx, q, secs, maxAttempts)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// LatestForPeriod returns the summary_jobs row for (user, period_type,
// period_start), or ErrSummaryJobNotFound when nothing has ever been
// scheduled. The FE polls this on SummaryDetail to render the
// regenerating-now banner and confirm the worker actually picks the
// job up — without it, a 202 from /regenerate is silent.
//
// At most one row ever exists per period thanks to the UNIQUE
// (user_id, period_type, period_start) constraint.
func (s *SummaryJobStore) LatestForPeriod(
	ctx context.Context,
	userID, periodType string,
	periodStart time.Time,
) (*domain.SummaryJob, error) {
	const q = `SELECT ` + summaryJobColumns + `
	             FROM summary_jobs
	            WHERE user_id = $1 AND period_type = $2 AND period_start = $3`
	row := s.DB.QueryRow(ctx, q, userID, periodType, periodStart)
	out, err := scanSummaryJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSummaryJobNotFound
	}
	return out, err
}

// LastEntryDate returns the most recent local_date the user has any
// journal entry for, or zero time if they've never written. The
// dormancy guard uses this to decide whether to auto-arm the next
// daily/weekly/monthly slot.
func (s *SummaryJobStore) LastEntryDate(ctx context.Context, userID string) (time.Time, error) {
	const q = `SELECT COALESCE(MAX(local_date), '0001-01-01'::date)
	             FROM journal_entries WHERE user_id = $1`
	var t time.Time
	if err := s.DB.QueryRow(ctx, q, userID).Scan(&t); err != nil {
		return time.Time{}, err
	}
	return t, nil
}
