package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// MemoryExtractionJobStore writes the per-day memory reconciliation
// queue. Lifecycle mirrors SummaryJobStore (pending → claimed →
// completed/skipped/failed) with one divergence: the success transition
// is NOT exposed here — MemoryStore.ApplyExtractionOps marks the row
// completed inside the same transaction as the memory writes, because
// memory ADDs are not idempotent and apply+mark must be atomic.
type MemoryExtractionJobStore struct {
	DB *pgxpool.Pool
}

func NewMemoryExtractionJobStore(db *pgxpool.Pool) *MemoryExtractionJobStore {
	return &MemoryExtractionJobStore{DB: db}
}

// ErrMemoryExtractionJobNotFound is returned when a job id doesn't exist.
var ErrMemoryExtractionJobNotFound = errors.New("memory extraction job not found")

const memoryExtractionJobColumns = `id, user_id,
    to_char(local_date, 'YYYY-MM-DD') AS local_date,
    fire_at, fired_at, status, attempts, last_error, created_at, updated_at`

func scanMemoryExtractionJob(row pgx.Row) (*domain.MemoryExtractionJob, error) {
	var j domain.MemoryExtractionJob
	if err := row.Scan(
		&j.ID, &j.UserID, &j.LocalDate,
		&j.FireAt, &j.FiredAt, &j.Status, &j.Attempts, &j.LastError,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &j, nil
}

// Schedule lazily inserts a job row for (user, local_date). ON CONFLICT
// DO NOTHING — repeated lazy-seed calls (every entry write that day)
// can't double-schedule or move an already-armed fire time. Returns
// whether a new row was inserted.
func (s *MemoryExtractionJobStore) Schedule(
	ctx context.Context, userID string, localDate, fireAt time.Time,
) (bool, error) {
	const q = `
		INSERT INTO memory_extraction_jobs (user_id, local_date, fire_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, local_date) DO NOTHING
		RETURNING id`
	var id string
	err := s.DB.QueryRow(ctx, q, userID, localDate, fireAt).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ClaimedMemoryJob is the dispatcher's view of a row it just claimed —
// just enough to enqueue a River job; the worker reloads by id.
type ClaimedMemoryJob struct {
	ID        string
	UserID    string
	LocalDate string
}

// ClaimDue atomically marks up to `limit` due rows as 'claimed' and
// bumps attempts. FOR UPDATE SKIP LOCKED so concurrent dispatcher ticks
// (worker rolling-restart) don't double-enqueue.
func (s *MemoryExtractionJobStore) ClaimDue(ctx context.Context, limit int) ([]ClaimedMemoryJob, error) {
	const q = `
		UPDATE memory_extraction_jobs
		   SET status='claimed',
		       attempts = attempts + 1,
		       updated_at = now()
		 WHERE id IN (
		     SELECT id FROM memory_extraction_jobs
		      WHERE status='pending' AND fire_at <= now()
		      ORDER BY fire_at ASC
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, user_id, to_char(local_date, 'YYYY-MM-DD') AS local_date`
	rows, err := s.DB.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ClaimedMemoryJob, 0)
	for rows.Next() {
		var c ClaimedMemoryJob
		if err := rows.Scan(&c.ID, &c.UserID, &c.LocalDate); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetByID loads a job for the worker. No user scoping — the worker is
// trusted code and the id was just produced by the dispatcher.
func (s *MemoryExtractionJobStore) GetByID(ctx context.Context, id string) (*domain.MemoryExtractionJob, error) {
	const q = `SELECT ` + memoryExtractionJobColumns + ` FROM memory_extraction_jobs WHERE id = $1`
	out, err := scanMemoryExtractionJob(s.DB.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMemoryExtractionJobNotFound
	}
	return out, err
}

// MarkSkipped: the day had no journal record, so we didn't call the LLM.
func (s *MemoryExtractionJobStore) MarkSkipped(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE memory_extraction_jobs
		    SET status='skipped', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// ReleaseForRetry reverts a claimed job to pending so the dispatcher
// re-claims it next tick (River retry path). last_error preserves the
// diagnostic.
func (s *MemoryExtractionJobStore) ReleaseForRetry(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE memory_extraction_jobs
		    SET status='pending', last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}

// MarkFailed is the terminal failure path: River exhausted retries.
func (s *MemoryExtractionJobStore) MarkFailed(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE memory_extraction_jobs
		    SET status='failed', fired_at=now(), last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}

// ReclaimStale flips 'claimed' rows whose worker side appears stuck —
// updated_at older than staleAfter — back to 'pending'. Same recovery
// path as SummaryJobStore.ReclaimStale (worker crash / River job lost
// across a deploy). Bounded by attempts < maxAttempts.
func (s *MemoryExtractionJobStore) ReclaimStale(
	ctx context.Context, staleAfter time.Duration, maxAttempts int,
) (int, error) {
	secs := int(staleAfter.Seconds())
	if secs < 1 {
		secs = 1
	}
	const q = `
		UPDATE memory_extraction_jobs
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
