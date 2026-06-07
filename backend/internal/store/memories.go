package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// MemoryStore reads and writes the per-user durable-fact list
// (user_memories). Two write paths with different authority:
//
//   - The management API (user): Create / UpdateContentUser / SoftDelete.
//     User writes always win — Create and UpdateContentUser stamp
//     pinned=true, and SoftDelete is allowed on any active row.
//   - The reconciliation worker: ApplyExtractionOps. May only touch
//     active, non-pinned rows; runs as one transaction together with the
//     memory_extraction_jobs completion so a re-claimed job can never
//     double-apply (ADD is not idempotent on its own).
type MemoryStore struct {
	DB *pgxpool.Pool
}

func NewMemoryStore(db *pgxpool.Pool) *MemoryStore { return &MemoryStore{DB: db} }

// ErrMemoryNotFound is returned when a target id isn't owned by the
// caller (or doesn't exist, or isn't active).
var ErrMemoryNotFound = errors.New("memory not found")

const memoryColumns = `id, user_id, category, content, status, pinned, source,
    superseded_by,
    to_char(source_local_date, 'YYYY-MM-DD') AS source_local_date,
    created_at, updated_at`

func scanMemory(row pgx.Row) (*domain.Memory, error) {
	var m domain.Memory
	if err := row.Scan(
		&m.ID, &m.UserID, &m.Category, &m.Content, &m.Status, &m.Pinned, &m.Source,
		&m.SupersededBy, &m.SourceLocalDate,
		&m.CreatedAt, &m.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &m, nil
}

// ListActive returns every active memory for the user, ordered by
// canonical category position, pinned-first, oldest-first. Used by chat
// prompt injection, the reconciliation prompt, and the management UI.
func (s *MemoryStore) ListActive(ctx context.Context, userID string) ([]domain.Memory, error) {
	const q = `SELECT ` + memoryColumns + `
	             FROM user_memories
	            WHERE user_id = $1 AND status = 'active'
	            ORDER BY array_position(
	                       ARRAY['identity','relationships','work','health',
	                             'preferences','goals','routines','other'],
	                       category),
	                     pinned DESC, created_at ASC`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Memory, 0)
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetByID returns the memory scoped to userID, or nil if not found /
// wrong tenant (any status — callers filter).
func (s *MemoryStore) GetByID(ctx context.Context, userID, id string) (*domain.Memory, error) {
	const q = `SELECT ` + memoryColumns + `
	             FROM user_memories
	            WHERE id = $1 AND user_id = $2`
	m, err := scanMemory(s.DB.QueryRow(ctx, q, id, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return m, err
}

// Create inserts a user-authored memory: source='user', pinned=true, so
// the reconciliation worker will never touch it. Caller validates
// category + content bounds (the CHECK constraints backstop).
func (s *MemoryStore) Create(
	ctx context.Context, userID, category, content string,
) (*domain.Memory, error) {
	const q = `
		INSERT INTO user_memories (user_id, category, content, source, pinned)
		VALUES ($1, $2, $3, 'user', true)
		RETURNING ` + memoryColumns
	return scanMemory(s.DB.QueryRow(ctx, q, userID, category, content))
}

// UpdateContentUser is the management-UI edit path: in-place update that
// also flips the row to pinned=true / source='user' — a user edit makes
// the fact authoritative and locks it against the worker (manual-wins).
// No lineage row is written; the user's version simply replaces the text.
// Returns ErrMemoryNotFound if the row isn't the caller's or isn't active.
func (s *MemoryStore) UpdateContentUser(
	ctx context.Context, userID, id, category, content string,
) (*domain.Memory, error) {
	const q = `
		UPDATE user_memories
		   SET category = $3,
		       content = $4,
		       pinned = true,
		       source = 'user',
		       updated_at = now()
		 WHERE id = $1 AND user_id = $2 AND status = 'active'
		RETURNING ` + memoryColumns
	m, err := scanMemory(s.DB.QueryRow(ctx, q, id, userID, category, content))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMemoryNotFound
	}
	return m, err
}

// SoftDelete flips an active memory to status='deleted'. User path —
// pinned rows are deletable here (the pin only protects against the
// worker). Returns ErrMemoryNotFound when nothing matched.
func (s *MemoryStore) SoftDelete(ctx context.Context, userID, id string) error {
	tag, err := s.DB.Exec(ctx,
		`UPDATE user_memories
		    SET status='deleted', updated_at=now()
		  WHERE id = $1 AND user_id = $2 AND status = 'active'`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMemoryNotFound
	}
	return nil
}

// ApplyExtractionOps applies the validated reconciliation ops for one
// (user, local_date) pass and marks the memory_extraction_jobs row
// completed — all in a single transaction. That same-tx completion is a
// deliberate divergence from summary_jobs/chat_extraction_jobs (which
// mark outside any tx): memory ADDs are not idempotent, so apply+mark
// must be all-or-nothing for a re-claimed job to be safe. Either the
// whole pass committed (job is terminal, re-run early-exits in the
// worker) or none of it did (job re-runs cleanly).
//
// Per-op guards re-check the target inside the tx with FOR UPDATE:
// update/delete only land on rows that are still active AND NOT pinned
// — this closes the race where the user pins or edits a memory between
// prompt-build and apply. Ops whose target fails the re-check are
// silently skipped (the next day's pass sees the current state).
func (s *MemoryStore) ApplyExtractionOps(
	ctx context.Context,
	userID string,
	localDate time.Time,
	ops []domain.MemoryOp,
	jobID string,
) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, op := range ops {
		switch op.Op {
		case domain.MemoryOpAdd:
			if _, err := tx.Exec(ctx,
				`INSERT INTO user_memories
				     (user_id, category, content, source, pinned, source_local_date)
				 VALUES ($1, $2, $3, 'extraction', false, $4)`,
				userID, op.Category, op.Content, localDate); err != nil {
				return fmt.Errorf("apply add: %w", err)
			}

		case domain.MemoryOpUpdate:
			var oldID string
			err := tx.QueryRow(ctx,
				`SELECT id FROM user_memories
				  WHERE id = $1 AND user_id = $2 AND status = 'active' AND NOT pinned
				  FOR UPDATE`, op.ID, userID).Scan(&oldID)
			if errors.Is(err, pgx.ErrNoRows) {
				continue // pinned/edited/deleted since prompt-build — skip
			}
			if err != nil {
				return fmt.Errorf("lock update target: %w", err)
			}
			var newID string
			if err := tx.QueryRow(ctx,
				`INSERT INTO user_memories
				     (user_id, category, content, source, pinned, source_local_date)
				 VALUES ($1, $2, $3, 'extraction', false, $4)
				 RETURNING id`,
				userID, op.Category, op.Content, localDate).Scan(&newID); err != nil {
				return fmt.Errorf("apply update insert: %w", err)
			}
			if _, err := tx.Exec(ctx,
				`UPDATE user_memories
				    SET status='superseded', superseded_by=$3, updated_at=now()
				  WHERE id = $1 AND user_id = $2`, oldID, userID, newID); err != nil {
				return fmt.Errorf("apply update supersede: %w", err)
			}

		case domain.MemoryOpDelete:
			if _, err := tx.Exec(ctx,
				`UPDATE user_memories
				    SET status='deleted', updated_at=now()
				  WHERE id = $1 AND user_id = $2 AND status = 'active' AND NOT pinned`,
				op.ID, userID); err != nil {
				return fmt.Errorf("apply delete: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE memory_extraction_jobs
		    SET status='completed', fired_at=now(), last_error=NULL, updated_at=now()
		  WHERE id = $1`, jobID); err != nil {
		return fmt.Errorf("mark job completed: %w", err)
	}

	return tx.Commit(ctx)
}
