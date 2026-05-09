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

// ChatSessionStore reads and writes chat_sessions. Idempotency anchor is
// UNIQUE (user_id, local_date) — one session per day, mode-mutable.
type ChatSessionStore struct {
	DB *pgxpool.Pool
}

func NewChatSessionStore(db *pgxpool.Pool) *ChatSessionStore { return &ChatSessionStore{DB: db} }

// ErrChatSessionNotFound is returned when the requested id (or
// (user, date) pair) doesn't exist or doesn't belong to the caller.
// Surfaced as 404.
var ErrChatSessionNotFound = errors.New("chat session not found")

// ErrChatSessionInvalidPhase is returned by AdvancePhase when the
// requested transition is not legal per domain.LegalChatPhaseTransition.
// Surfaced as 409 to the caller.
var ErrChatSessionInvalidPhase = errors.New("invalid chat session phase transition")

const chatSessionColumns = `id, user_id,
    to_char(local_date, 'YYYY-MM-DD') AS local_date,
    mode, phase, chat_model, extraction_model, openai_session_id,
    started_at, last_activity_at, ended_at, finalized_at,
    extraction_status, extraction_error, covered_question_ids,
    coverage_last_classified_seq,
    created_at, updated_at`

func scanChatSession(row pgx.Row) (*domain.ChatSession, error) {
	var s domain.ChatSession
	if err := row.Scan(
		&s.ID, &s.UserID, &s.LocalDate, &s.Mode, &s.Phase,
		&s.ChatModel, &s.ExtractionModel, &s.OpenAISessionID,
		&s.StartedAt, &s.LastActivityAt, &s.EndedAt, &s.FinalizedAt,
		&s.ExtractionStatus, &s.ExtractionError, &s.CoveredQuestionIDs,
		&s.CoverageLastClassifiedSeq,
		&s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &s, nil
}

// CreateOrResume returns the (user, local_date) session, creating one if
// none exists. Idempotent — concurrent calls converge on the same row
// thanks to UNIQUE (user_id, local_date). chatModel / extractionModel
// are persisted on insert only; if the row already existed we return
// it untouched (model swaps mid-day apply to new sessions only, not
// retroactively).
//
// Returns (session, created bool, error). `created=true` means the
// caller is the first to open today's session — useful for "stream the
// opener" gating on the handler.
func (s *ChatSessionStore) CreateOrResume(
	ctx context.Context,
	userID string,
	localDate time.Time,
	chatModel, extractionModel string,
) (*domain.ChatSession, bool, error) {
	const q = `
		INSERT INTO chat_sessions (user_id, local_date, chat_model, extraction_model)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, local_date) DO UPDATE
		   SET updated_at = chat_sessions.updated_at  -- no-op to RETURN existing row
		RETURNING ` + chatSessionColumns + `,
		         (xmax = 0) AS inserted`
	row := s.DB.QueryRow(ctx, q, userID, localDate, chatModel, extractionModel)
	var sess domain.ChatSession
	var inserted bool
	if err := row.Scan(
		&sess.ID, &sess.UserID, &sess.LocalDate, &sess.Mode, &sess.Phase,
		&sess.ChatModel, &sess.ExtractionModel, &sess.OpenAISessionID,
		&sess.StartedAt, &sess.LastActivityAt, &sess.EndedAt, &sess.FinalizedAt,
		&sess.ExtractionStatus, &sess.ExtractionError, &sess.CoveredQuestionIDs,
		&sess.CoverageLastClassifiedSeq,
		&sess.CreatedAt, &sess.UpdatedAt,
		&inserted,
	); err != nil {
		return nil, false, err
	}
	return &sess, inserted, nil
}

// GetByID loads a session scoped to user. Returns ErrChatSessionNotFound
// for cross-tenant ids — same defense-in-depth pattern as the other
// per-user stores.
func (s *ChatSessionStore) GetByID(
	ctx context.Context, userID, id string,
) (*domain.ChatSession, error) {
	const q = `SELECT ` + chatSessionColumns + `
	             FROM chat_sessions
	            WHERE id = $1 AND user_id = $2`
	row := s.DB.QueryRow(ctx, q, id, userID)
	out, err := scanChatSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatSessionNotFound
	}
	return out, err
}

// GetByDate returns the row for one user/day, or ErrChatSessionNotFound
// if the user hasn't started a session for that day. The Today handler
// distinguishes 404 (which surfaces as `{session: null}`) from real
// errors.
func (s *ChatSessionStore) GetByDate(
	ctx context.Context, userID string, localDate time.Time,
) (*domain.ChatSession, error) {
	const q = `SELECT ` + chatSessionColumns + `
	             FROM chat_sessions
	            WHERE user_id = $1 AND local_date = $2`
	row := s.DB.QueryRow(ctx, q, userID, localDate)
	out, err := scanChatSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatSessionNotFound
	}
	return out, err
}

// GetByIDForWorker is an unscoped read for the extraction worker. The
// worker is trusted code — the job_id was just produced by the
// dispatcher, which already validated user ownership at scheduling time.
func (s *ChatSessionStore) GetByIDForWorker(ctx context.Context, id string) (*domain.ChatSession, error) {
	const q = `SELECT ` + chatSessionColumns + `
	             FROM chat_sessions WHERE id = $1`
	row := s.DB.QueryRow(ctx, q, id)
	out, err := scanChatSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrChatSessionNotFound
	}
	return out, err
}

// AdvancePhase moves a session to a new phase, validating the transition
// against domain.LegalChatPhaseTransition. Setting to 'finalized' or
// 'abandoned' also stamps ended_at (callers should call MarkFinalized
// for the success path so finalized_at is set too).
//
// Returns ErrChatSessionNotFound if the row doesn't exist;
// ErrChatSessionInvalidPhase if the transition is illegal.
func (s *ChatSessionStore) AdvancePhase(
	ctx context.Context, sessionID, newPhase string,
) (*domain.ChatSession, error) {
	if !domain.IsValidChatPhase(newPhase) {
		return nil, ErrChatSessionInvalidPhase
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var current string
	if err := tx.QueryRow(ctx,
		`SELECT phase FROM chat_sessions WHERE id = $1 FOR UPDATE`,
		sessionID,
	).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrChatSessionNotFound
		}
		return nil, err
	}
	if current == newPhase {
		// Idempotent re-advance — return current row unchanged.
		row := tx.QueryRow(ctx, `SELECT `+chatSessionColumns+` FROM chat_sessions WHERE id = $1`, sessionID)
		out, err := scanChatSession(row)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return out, nil
	}
	if !domain.LegalChatPhaseTransition(current, newPhase) {
		return nil, fmt.Errorf("%w: %s → %s", ErrChatSessionInvalidPhase, current, newPhase)
	}
	// Terminal phases stamp ended_at; the rest only update phase + updated_at.
	endedAtClause := ""
	if newPhase == domain.ChatPhaseFinalized || newPhase == domain.ChatPhaseAbandoned {
		endedAtClause = `, ended_at = COALESCE(ended_at, now())`
	}
	row := tx.QueryRow(ctx,
		`UPDATE chat_sessions
		    SET phase = $1, updated_at = now()`+endedAtClause+`
		  WHERE id = $2
		  RETURNING `+chatSessionColumns,
		newPhase, sessionID,
	)
	out, err := scanChatSession(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// MarkVoice flips a session into voice mode and stamps the OpenAI
// Realtime session id + model. Idempotent: re-issuing /voice/start for
// the same session updates the openai_session_id (a stale ephemeral
// secret may have expired) without changing user-visible state.
//
// Scoped by user_id as defense-in-depth even though the caller already
// loaded the session via GetByID.
func (s *ChatSessionStore) MarkVoice(
	ctx context.Context, userID, sessionID, model, openaiSessionID string,
) error {
	ct, err := s.DB.Exec(ctx,
		`UPDATE chat_sessions
		    SET mode              = 'voice',
		        openai_session_id = $3,
		        chat_model        = CASE WHEN $4 = '' THEN chat_model ELSE $4 END,
		        last_activity_at  = now(),
		        updated_at        = now()
		  WHERE id = $1 AND user_id = $2`,
		sessionID, userID, openaiSessionID, model)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrChatSessionNotFound
	}
	return nil
}

// TouchActivity bumps last_activity_at to now. Called from the streaming
// handler on every user message and assistant turn so the idle sweeper's
// 20-minute clock resets.
func (s *ChatSessionStore) TouchActivity(ctx context.Context, sessionID string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_sessions SET last_activity_at = now(), updated_at = now() WHERE id = $1`,
		sessionID)
	return err
}

// SetCoveredQuestionIDs overwrites the authoritative covered set
// computed by the post-turn classifier and advances the
// coverage_last_classified_seq watermark so the next turn can short-
// circuit when no new user content arrived. Empty slice is a valid
// value (nothing covered yet); the classifier runs in delta mode but
// the store always persists the full union.
//
// Race-safe: the WHERE clause skips the write when a newer
// classification has already landed (classifiedSeq < persisted
// watermark). This matters because the streaming handler runs the
// classifier on a detached context — a slow older run could otherwise
// clobber a newer run that already persisted.
func (s *ChatSessionStore) SetCoveredQuestionIDs(
	ctx context.Context, sessionID string, ids []string, classifiedSeq int,
) error {
	if ids == nil {
		ids = []string{}
	}
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_sessions
		    SET covered_question_ids         = $1,
		        coverage_last_classified_seq = $2,
		        updated_at                    = now()
		  WHERE id = $3
		    AND coverage_last_classified_seq <= $2`,
		ids, classifiedSeq, sessionID)
	return err
}

// SetExtractionStatus is the single mutator for extraction_status +
// extraction_error. The handler sets pending on enqueue; the worker
// sets running on claim and completed/failed on finish.
func (s *ChatSessionStore) SetExtractionStatus(
	ctx context.Context, sessionID, status string, errMsg *string,
) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_sessions
		    SET extraction_status = $1,
		        extraction_error  = $2,
		        updated_at        = now()
		  WHERE id = $3`,
		status, errMsg, sessionID)
	return err
}

// MarkFinalized stamps finalized_at + ended_at on the success path. The
// extraction worker calls this AFTER a successful upsert + AdvancePhase.
// finalized_at is the timestamp the UI uses to label the session
// "auto-filled at HH:MM"; ended_at is what AdvancePhase already set.
func (s *ChatSessionStore) MarkFinalized(ctx context.Context, sessionID string) error {
	_, err := s.DB.Exec(ctx,
		`UPDATE chat_sessions
		    SET finalized_at = now(),
		        ended_at     = COALESCE(ended_at, now()),
		        updated_at   = now()
		  WHERE id = $1`,
		sessionID)
	return err
}

// Reset wipes the session's transcript, rolls the phase back to
// greeting, and clears extraction status. Used for the destructive
// "Reset chat" affordance — the user has been warned. Does NOT touch
// daily_inputs or journal_entries from prior extractions; those are
// the user's saved data and stay put.
//
// Returns the resulting session row.
func (s *ChatSessionStore) Reset(ctx context.Context, userID, sessionID string) (*domain.ChatSession, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Confirm ownership before deleting anything.
	var owned int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_sessions WHERE id = $1 AND user_id = $2`,
		sessionID, userID,
	).Scan(&owned); err != nil {
		return nil, err
	}
	if owned == 0 {
		return nil, ErrChatSessionNotFound
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM chat_messages WHERE session_id = $1`, sessionID,
	); err != nil {
		return nil, fmt.Errorf("delete messages: %w", err)
	}
	// Drop any pending/claimed extraction job — a reset moots an in-flight
	// extraction. Completed jobs are already terminal so the cascade is
	// a no-op there; this just keeps the queue clean.
	if _, err := tx.Exec(ctx,
		`DELETE FROM chat_extraction_jobs WHERE session_id = $1`, sessionID,
	); err != nil {
		return nil, fmt.Errorf("delete extraction job: %w", err)
	}

	row := tx.QueryRow(ctx,
		`UPDATE chat_sessions
		    SET phase                        = 'greeting',
		        extraction_status            = 'idle',
		        extraction_error             = NULL,
		        ended_at                     = NULL,
		        finalized_at                 = NULL,
		        covered_question_ids         = ARRAY[]::text[],
		        coverage_last_classified_seq = 0,
		        last_activity_at             = now(),
		        updated_at                   = now()
		  WHERE id = $1
		  RETURNING `+chatSessionColumns,
		sessionID,
	)
	out, err := scanChatSession(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// ClaimIdleForFinalize atomically returns the ids of sessions whose
// last_activity_at is older than `idleBefore` AND whose phase is still
// non-terminal. Caller (the dispatcher tick) is expected to enqueue an
// extraction job for each id and advance the session to 'wrapping_up'
// in the same loop iteration.
//
// FOR UPDATE SKIP LOCKED keeps two dispatcher replicas from claiming
// the same session twice during a rolling restart.
//
// Returns rows in last_activity_at order so the oldest stranded
// sessions drain first.
func (s *ChatSessionStore) ClaimIdleForFinalize(
	ctx context.Context, idleBefore time.Time, limit int,
) ([]string, error) {
	const q = `
		SELECT id
		  FROM chat_sessions
		 WHERE last_activity_at < $1
		   AND phase IN ('greeting','exploring','wrapping_up')
		   AND extraction_status NOT IN ('pending','running','completed')
		 ORDER BY last_activity_at ASC
		 LIMIT $2
		   FOR UPDATE SKIP LOCKED`
	rows, err := s.DB.Query(ctx, q, idleBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
