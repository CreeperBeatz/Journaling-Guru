package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ReminderJob is the projection used by the worker after the dispatcher
// has handed it a row id. PeriodStart/PeriodEnd vocabulary (from
// SummaryJob) doesn't apply here — a reminder is a single instant.
type ReminderJob struct {
	ID        string
	UserID    string
	FireAt    time.Time
	FiredAt   *time.Time
	Status    string
	Attempts  int
	LastError *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReminderJobStore writes the per-user reminder queue. Lifecycle parallels
// SummaryJobStore — pending → claimed → sent / skipped / failed — so the
// dispatcher tick can drain both with the same atomic claim.
type ReminderJobStore struct {
	DB *pgxpool.Pool
}

func NewReminderJobStore(db *pgxpool.Pool) *ReminderJobStore { return &ReminderJobStore{DB: db} }

// ErrReminderJobNotFound is returned when a target id no longer exists
// (cascaded delete after user removal, etc.).
var ErrReminderJobNotFound = errors.New("reminder job not found")

const reminderJobColumns = `id, user_id, fire_at, fired_at, status,
    attempts, last_error, created_at, updated_at`

func scanReminderJob(row pgx.Row) (*ReminderJob, error) {
	var j ReminderJob
	if err := row.Scan(
		&j.ID, &j.UserID, &j.FireAt, &j.FiredAt, &j.Status,
		&j.Attempts, &j.LastError, &j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &j, nil
}

// Schedule inserts a reminder_jobs row. ON CONFLICT DO NOTHING is the
// expected case when settings change in two browser tabs at once — the
// later replan is a no-op.
//
// Returns whether a new row was inserted (true) or an existing one was
// preserved (false).
func (s *ReminderJobStore) Schedule(ctx context.Context, userID string, fireAt time.Time) (bool, error) {
	const q = `
		INSERT INTO reminder_jobs (user_id, fire_at)
		VALUES ($1, $2)
		ON CONFLICT (user_id, fire_at) DO NOTHING
		RETURNING id`
	var id string
	err := s.DB.QueryRow(ctx, q, userID, fireAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// DeletePendingForUser drops every still-pending row for a user. Called
// during settings replan: when reminder_time changes, yesterday's
// scheduling is no longer correct, so we wipe pending rows and
// schedule the next one fresh.
//
// Claimed/sent/skipped/failed rows are preserved — they are either
// in flight or part of the audit trail.
func (s *ReminderJobStore) DeletePendingForUser(ctx context.Context, userID string) error {
	_, err := s.DB.Exec(ctx,
		`DELETE FROM reminder_jobs WHERE user_id = $1 AND status = 'pending'`,
		userID)
	return err
}

// ClaimedReminder is the dispatcher's view of a claimed row.
type ClaimedReminder struct {
	ID     string
	UserID string
}

// ClaimDue atomically marks up to `limit` due rows as 'claimed' and
// bumps attempts. Same pattern as SummaryJobStore.ClaimDue — concurrent
// dispatchers can't double-enqueue.
func (s *ReminderJobStore) ClaimDue(ctx context.Context, limit int) ([]ClaimedReminder, error) {
	const q = `
		UPDATE reminder_jobs
		   SET status     = 'claimed',
		       attempts   = attempts + 1,
		       updated_at = now()
		 WHERE id IN (
		     SELECT id FROM reminder_jobs
		      WHERE status = 'pending' AND fire_at <= now()
		      ORDER BY fire_at ASC
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, user_id`
	rows, err := s.DB.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ClaimedReminder, 0)
	for rows.Next() {
		var c ClaimedReminder
		if err := rows.Scan(&c.ID, &c.UserID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetByID loads a job for the worker.
func (s *ReminderJobStore) GetByID(ctx context.Context, id string) (*ReminderJob, error) {
	const q = `SELECT ` + reminderJobColumns + ` FROM reminder_jobs WHERE id = $1`
	out, err := scanReminderJob(s.DB.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrReminderJobNotFound
	}
	return out, err
}

// MarkSent: at least one push delivered. The worker calls this
// regardless of how many subscriptions failed, as long as one succeeded
// — the user "got the reminder" in any practical sense.
func (s *ReminderJobStore) MarkSent(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE reminder_jobs
		    SET status='sent', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// MarkSkipped: nothing to deliver to (no subscriptions, or
// reminder_enabled flipped off between scheduling and firing). We
// still schedule tomorrow — re-subscribing should auto-resume.
func (s *ReminderJobStore) MarkSkipped(ctx context.Context, id, reason string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE reminder_jobs
		    SET status='skipped', fired_at=now(), last_error=$2, updated_at=now()
		  WHERE id = $1`, id, reason)
	return err
}

// ReleaseForRetry reverts a claimed row to pending so the dispatcher
// re-claims it next tick. Used when the worker hits a transient error
// (push service 5xx, network blip).
func (s *ReminderJobStore) ReleaseForRetry(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE reminder_jobs
		    SET status='pending', last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}

// MarkFailed is the terminal failure path: River exhausted retries.
func (s *ReminderJobStore) MarkFailed(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE reminder_jobs
		    SET status='failed', fired_at=now(), last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}
