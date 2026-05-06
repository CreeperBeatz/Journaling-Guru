package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// QuestionStore is the only path that reads or writes `questions`. Every
// query is scoped by user_id; nothing leaks across tenants.
type QuestionStore struct {
	DB *pgxpool.Pool
}

func NewQuestionStore(db *pgxpool.Pool) *QuestionStore { return &QuestionStore{DB: db} }

// ErrQuestionNotFound is returned by ops that target an id which doesn't
// belong to the caller (or is archived). Surfaced as 404 by the handler.
var ErrQuestionNotFound = errors.New("question not found")

const questionColumns = `id, user_id, prompt, position, archived_at, created_at, updated_at`

func scanQuestion(row pgx.Row) (*domain.Question, error) {
	var q domain.Question
	if err := row.Scan(&q.ID, &q.UserID, &q.Prompt, &q.Position, &q.ArchivedAt, &q.CreatedAt, &q.UpdatedAt); err != nil {
		return nil, err
	}
	return &q, nil
}

// ListActive returns the user's non-archived questions, ordered by
// position ascending. Empty slice if the user has none — the handler
// decides whether to seed defaults.
func (s *QuestionStore) ListActive(ctx context.Context, userID string) ([]domain.Question, error) {
	const q = `SELECT ` + questionColumns + `
	  FROM questions
	 WHERE user_id = $1 AND archived_at IS NULL
	 ORDER BY position ASC`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Question, 0)
	for rows.Next() {
		var item domain.Question
		if err := rows.Scan(&item.ID, &item.UserID, &item.Prompt, &item.Position, &item.ArchivedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// SeedDefaults inserts `prompts` for `userID` at positions 0..N-1. Runs
// inside a tx; aborts cleanly if any single insert collides (e.g. a
// concurrent first-load — second caller will just see the first's seed).
func (s *QuestionStore) SeedDefaults(ctx context.Context, userID string, prompts []string) ([]domain.Question, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	out := make([]domain.Question, 0, len(prompts))
	for i, p := range prompts {
		row := tx.QueryRow(ctx,
			`INSERT INTO questions (user_id, prompt, position) VALUES ($1, $2, $3)
			 RETURNING `+questionColumns,
			userID, p, i,
		)
		q, err := scanQuestion(row)
		if err != nil {
			return nil, fmt.Errorf("seed question %d: %w", i, err)
		}
		out = append(out, *q)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// Create appends a new question at the end (max position + 1). Returns the
// inserted row.
func (s *QuestionStore) Create(ctx context.Context, userID, prompt string) (*domain.Question, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var nextPos int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(position) + 1, 0) FROM questions WHERE user_id = $1`,
		userID,
	).Scan(&nextPos); err != nil {
		return nil, err
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO questions (user_id, prompt, position) VALUES ($1, $2, $3)
		 RETURNING `+questionColumns,
		userID, prompt, nextPos,
	)
	q, err := scanQuestion(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return q, nil
}

// UpdatePrompt edits the text of an existing question. Returns
// ErrQuestionNotFound when the row doesn't belong to userID or is
// archived.
func (s *QuestionStore) UpdatePrompt(ctx context.Context, userID, id, prompt string) (*domain.Question, error) {
	row := s.DB.QueryRow(ctx,
		`UPDATE questions
		    SET prompt = $1, updated_at = now()
		  WHERE id = $2 AND user_id = $3 AND archived_at IS NULL
		  RETURNING `+questionColumns,
		prompt, id, userID,
	)
	q, err := scanQuestion(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrQuestionNotFound
	}
	return q, err
}

// Archive soft-deletes the question and re-packs sibling positions so the
// active set stays gap-free. Runs in a tx with the deferred unique
// constraint so the multi-row UPDATE is allowed mid-statement.
func (s *QuestionStore) Archive(ctx context.Context, userID, id string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var pos int
	err = tx.QueryRow(ctx,
		`UPDATE questions
		    SET archived_at = now(), updated_at = now()
		  WHERE id = $1 AND user_id = $2 AND archived_at IS NULL
		  RETURNING position`,
		id, userID,
	).Scan(&pos)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrQuestionNotFound
	}
	if err != nil {
		return err
	}

	// Bump archived row's position out of the way before re-packing so it
	// can't conflict with a sibling. The deferred unique constraint
	// validates at commit, so transient duplicates inside the tx are fine,
	// but using a sentinel keeps intent obvious.
	if _, err := tx.Exec(ctx,
		`UPDATE questions SET position = -1 WHERE id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE questions
		    SET position = position - 1, updated_at = now()
		  WHERE user_id = $1 AND archived_at IS NULL AND position > $2`,
		userID, pos,
	); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Reorder rewrites positions to match `orderedIDs`. All ids must belong to
// the user and be currently active; otherwise the whole tx aborts and
// returns ErrQuestionNotFound. We rely on the deferred UNIQUE constraint —
// transient collisions during the pass-through update are allowed and only
// the final state is validated at commit.
func (s *QuestionStore) Reorder(ctx context.Context, userID string, orderedIDs []string) error {
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Verify every id is owned + active before we start mutating.
	var owned int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM questions
		  WHERE user_id = $1 AND archived_at IS NULL AND id = ANY($2::uuid[])`,
		userID, orderedIDs,
	).Scan(&owned); err != nil {
		return err
	}
	if owned != len(orderedIDs) {
		return ErrQuestionNotFound
	}

	for i, id := range orderedIDs {
		if _, err := tx.Exec(ctx,
			`UPDATE questions SET position = $1, updated_at = now()
			  WHERE id = $2 AND user_id = $3`,
			i, id, userID,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
