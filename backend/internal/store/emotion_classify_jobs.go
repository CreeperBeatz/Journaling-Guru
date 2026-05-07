package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// EmotionClassifyJobStore writes the queue for the Plutchik-wheel
// classifier. Lifecycle and dispatcher pattern mirror SummaryJobStore;
// see 0006_emotion_classification.sql for the table comment.
type EmotionClassifyJobStore struct {
	DB *pgxpool.Pool
}

func NewEmotionClassifyJobStore(db *pgxpool.Pool) *EmotionClassifyJobStore {
	return &EmotionClassifyJobStore{DB: db}
}

// ErrEmotionClassifyJobNotFound is returned when an id no longer exists
// (cascaded delete after user removal, etc.).
var ErrEmotionClassifyJobNotFound = errors.New("emotion classify job not found")

const emotionClassifyJobColumns = `id, user_id,
    to_char(local_date, 'YYYY-MM-DD') AS local_date,
    fire_at, fired_at, status, attempts, last_error, created_at, updated_at`

func scanEmotionClassifyJob(row pgx.Row) (*domain.EmotionClassifyJob, error) {
	var j domain.EmotionClassifyJob
	if err := row.Scan(
		&j.ID, &j.UserID, &j.LocalDate,
		&j.FireAt, &j.FiredAt, &j.Status, &j.Attempts, &j.LastError,
		&j.CreatedAt, &j.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &j, nil
}

// Schedule inserts (or revives) a row for (user_id, local_date). On
// conflict the row is reset to pending unless it's currently claimed —
// an in-flight worker still has the old emotions_text and will write
// stale output if interrupted, so we let it finish; the next save by
// the user will re-arm via a fresh Schedule call.
//
// Returns true when a new run was effectively armed (insert OR revival
// of a terminal row), false when the existing row was preserved
// (currently in flight).
func (s *EmotionClassifyJobStore) Schedule(
	ctx context.Context,
	userID string,
	localDate time.Time,
	fireAt time.Time,
) (bool, error) {
	const q = `
		INSERT INTO emotion_classify_jobs (user_id, local_date, fire_at, status, attempts)
		VALUES ($1, $2, $3, 'pending', 0)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET fire_at    = EXCLUDED.fire_at,
		       status     = 'pending',
		       attempts   = 0,
		       last_error = NULL,
		       updated_at = now()
		 WHERE emotion_classify_jobs.status <> 'claimed'
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

// ClaimedEmotionJob is the dispatcher's view of a claimed row.
type ClaimedEmotionJob struct {
	ID        string
	UserID    string
	LocalDate string
}

// ClaimDue atomically marks up to `limit` due rows as 'claimed' and
// bumps their attempts counter. FOR UPDATE SKIP LOCKED keeps two
// dispatcher ticks (e.g. during rolling restart) from double-enqueuing.
func (s *EmotionClassifyJobStore) ClaimDue(ctx context.Context, limit int) ([]ClaimedEmotionJob, error) {
	const q = `
		UPDATE emotion_classify_jobs
		   SET status='claimed',
		       attempts = attempts + 1,
		       updated_at = now()
		 WHERE id IN (
		     SELECT id FROM emotion_classify_jobs
		      WHERE status='pending' AND fire_at <= now()
		      ORDER BY fire_at ASC
		      LIMIT $1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING id, user_id,
		          to_char(local_date, 'YYYY-MM-DD') AS local_date`
	rows, err := s.DB.Query(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ClaimedEmotionJob, 0)
	for rows.Next() {
		var c ClaimedEmotionJob
		if err := rows.Scan(&c.ID, &c.UserID, &c.LocalDate); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetByID loads a job for the worker. No user scoping — the worker is
// trusted code, and the id was just produced by the dispatcher.
func (s *EmotionClassifyJobStore) GetByID(ctx context.Context, id string) (*domain.EmotionClassifyJob, error) {
	const q = `SELECT ` + emotionClassifyJobColumns + ` FROM emotion_classify_jobs WHERE id = $1`
	row := s.DB.QueryRow(ctx, q, id)
	out, err := scanEmotionClassifyJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrEmotionClassifyJobNotFound
	}
	return out, err
}

// MarkCompleted: classifier wrote classified_emotions.
func (s *EmotionClassifyJobStore) MarkCompleted(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE emotion_classify_jobs
		    SET status='completed', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// MarkSkipped: emotions_text was empty (or row vanished); no LLM call.
func (s *EmotionClassifyJobStore) MarkSkipped(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE emotion_classify_jobs
		    SET status='skipped', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, id)
	return err
}

// ReleaseForRetry reverts a claimed job to pending so the dispatcher
// will re-claim it next tick. Used when River retries the worker.
func (s *EmotionClassifyJobStore) ReleaseForRetry(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE emotion_classify_jobs
		    SET status='pending', last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}

// MarkFailed is the terminal failure path: River exhausted retries.
func (s *EmotionClassifyJobStore) MarkFailed(ctx context.Context, id, lastError string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE emotion_classify_jobs
		    SET status='failed', fired_at=now(), last_error=$2, updated_at=now()
		  WHERE id = $1`, id, lastError)
	return err
}
