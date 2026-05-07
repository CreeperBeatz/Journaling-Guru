package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// ChatExtractionJobStore mirrors SummaryJobStore's lifecycle (pending →
// claimed → completed/skipped/failed) but is keyed by session_id (UNIQUE
// per session). Re-arming a failed extraction is the Regenerate path —
// ON CONFLICT DO UPDATE resets status + attempts.
type ChatExtractionJobStore struct {
	DB *pgxpool.Pool
}

func NewChatExtractionJobStore(db *pgxpool.Pool) *ChatExtractionJobStore {
	return &ChatExtractionJobStore{DB: db}
}

// ErrChatExtractionJobNotFound is returned when the requested id doesn't
// exist (race against a delete cascade). Callers ack and don't retry.
var ErrChatExtractionJobNotFound = errors.New("chat extraction job not found")

const chatExtractionJobColumns = `id, session_id, user_id,
    fire_at, fired_at, status, attempts, last_error, created_at, updated_at`

func scanChatExtractionJob(row pgx.Row) (*domain.ChatExtractionJob, error) {
	var j domain.ChatExtractionJob
	if err := row.Scan(
		&j.ID, &j.SessionID, &j.UserID,
		&j.FireAt, &j.FiredAt, &j.Status, &j.Attempts, &j.LastError,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &j, nil
}

// Schedule inserts (or re-arms) a chat_extraction_jobs row for the
// session. Idempotent on session_id; if a row already exists in a
// terminal state (completed/skipped/failed) we re-arm it. Pending or
// claimed rows are left untouched so a double-finalize click within
// the same minute doesn't multiply attempts.
//
// Returns whether the operation produced an effective trigger (true)
// or was a no-op (false).
func (s *ChatExtractionJobStore) Schedule(
	ctx context.Context, sessionID, userID string,
) (bool, error) {
	const q = `
		INSERT INTO chat_extraction_jobs (session_id, user_id)
		VALUES ($1, $2)
		ON CONFLICT (session_id) DO UPDATE
		   SET status     = 'pending',
		       fire_at    = now(),
		       fired_at   = NULL,
		       attempts   = 0,
		       last_error = NULL,
		       updated_at = now()
		 WHERE chat_extraction_jobs.status NOT IN ('pending','claimed')
		RETURNING id`
	var id string
	err := s.DB.QueryRow(ctx, q, sessionID, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// ClaimedChatExtractionJob is the dispatcher's view of a row it just
// claimed. Just enough to enqueue a River job — the worker reads the
// full row by id.
type ClaimedChatExtractionJob struct {
	ID        string
	SessionID string
	UserID    string
}

// ClaimDue marks up to `limit` due rows as 'claimed' and returns them.
// Same FOR UPDATE SKIP LOCKED pattern as SummaryJobStore.ClaimDue —
// concurrent dispatcher replicas can't double-claim.
func (s *ChatExtractionJobStore) ClaimDue(
	ctx context.Context, limit int,
) ([]ClaimedChatExtractionJob, error) {
	const q = `
		UPDATE chat_extraction_jobs
		   SET status     = 'claimed',
		       attempts   = attempts + 1,
		       updated_at = now()
		 WHERE id IN (
		     SELECT id FROM chat_extraction_jobs
		      WHERE status = 'pending' AND fire_at <= now()
		      ORDER BY fire_at ASC
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, session_id, user_id`
	rows, err := s.DB.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ClaimedChatExtractionJob, 0)
	for rows.Next() {
		var c ClaimedChatExtractionJob
		if err := rows.Scan(&c.ID, &c.SessionID, &c.UserID); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetByID loads a job for the worker. Unscoped — the worker is trusted
// code, the job_id was just produced by the dispatcher.
func (s *ChatExtractionJobStore) GetByID(ctx context.Context, id string) (*domain.ChatExtractionJob, error) {
	const q = `SELECT ` + chatExtractionJobColumns + ` FROM chat_extraction_jobs WHERE id = $1`
	row := s.DB.QueryRow(ctx, q, id)
	out, err := scanChatExtractionJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatExtractionJobNotFound
	}
	return out, err
}

// GetBySessionID is used by the /extraction/status poller endpoint. The
// session_id is already user-scoped via the session row.
func (s *ChatExtractionJobStore) GetBySessionID(ctx context.Context, sessionID string) (*domain.ChatExtractionJob, error) {
	const q = `SELECT ` + chatExtractionJobColumns + ` FROM chat_extraction_jobs WHERE session_id = $1`
	row := s.DB.QueryRow(ctx, q, sessionID)
	out, err := scanChatExtractionJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatExtractionJobNotFound
	}
	return out, err
}

// MarkCompleted is the success path: extraction wrote daily_inputs and
// journal_entries.
func (s *ChatExtractionJobStore) MarkCompleted(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_extraction_jobs
		    SET status='completed', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// MarkSkipped: the session had no usable transcript (idle sweeper claimed
// a session with only a greeting message, e.g.). Daily_inputs untouched.
func (s *ChatExtractionJobStore) MarkSkipped(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_extraction_jobs
		    SET status='skipped', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// ReleaseForRetry reverts a claimed job to pending so the dispatcher
// re-claims next tick. Symmetric with SummaryJobStore.ReleaseForRetry.
func (s *ChatExtractionJobStore) ReleaseForRetry(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_extraction_jobs
		    SET status='pending', last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}

// MarkFailed is the terminal failure path: River exhausted retries.
func (s *ChatExtractionJobStore) MarkFailed(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_extraction_jobs
		    SET status='failed', fired_at=now(), last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}
