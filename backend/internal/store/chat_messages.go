package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// ChatMessageStore writes and reads the per-session transcript. Ordering
// is explicit via `seq` (assigned under FOR UPDATE on the session row to
// keep the order monotonic across concurrent writers).
type ChatMessageStore struct {
	DB *pgxpool.Pool
}

func NewChatMessageStore(db *pgxpool.Pool) *ChatMessageStore { return &ChatMessageStore{DB: db} }

const chatMessageColumns = `id, session_id, seq, role, content,
    tool_name, tool_args, tool_result, token_in, token_out, created_at`

// AppendInput is the parameter struct for ChatMessageStore.Append. Every
// field except SessionID and Role is optional; Content defaults to "".
type AppendInput struct {
	SessionID  string
	Role       string
	Content    string
	ToolName   *string
	ToolArgs   map[string]any
	ToolResult map[string]any
	TokenIn    int
	TokenOut   int
}

// Append assigns the next seq inside the session under FOR UPDATE on the
// session row to keep seq monotonic across concurrent writers (rare but
// possible — the streaming handler holds a long-lived connection while
// the idle sweeper might inject a system_event row in parallel).
//
// Returns the persisted row so the SSE handler can echo `event: done`
// with the assistant message id.
func (s *ChatMessageStore) Append(ctx context.Context, in AppendInput) (*domain.ChatMessage, error) {
	if !isValidChatRole(in.Role) {
		return nil, fmt.Errorf("invalid chat role %q", in.Role)
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Lock the session to serialize seq assignment. Cheap — sessions are
	// per-user-per-day so contention is bounded.
	if _, err := tx.Exec(ctx,
		`SELECT 1 FROM chat_sessions WHERE id = $1 FOR UPDATE`, in.SessionID,
	); err != nil {
		return nil, fmt.Errorf("lock session: %w", err)
	}
	var nextSeq int
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(seq), 0) + 1 FROM chat_messages WHERE session_id = $1`,
		in.SessionID,
	).Scan(&nextSeq); err != nil {
		return nil, fmt.Errorf("compute next seq: %w", err)
	}

	var argsJSON, resultJSON []byte
	if in.ToolArgs != nil {
		argsJSON, err = json.Marshal(in.ToolArgs)
		if err != nil {
			return nil, fmt.Errorf("marshal tool_args: %w", err)
		}
	}
	if in.ToolResult != nil {
		resultJSON, err = json.Marshal(in.ToolResult)
		if err != nil {
			return nil, fmt.Errorf("marshal tool_result: %w", err)
		}
	}

	row := tx.QueryRow(ctx,
		`INSERT INTO chat_messages
		    (session_id, seq, role, content, tool_name, tool_args, tool_result, token_in, token_out)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+chatMessageColumns,
		in.SessionID, nextSeq, in.Role, in.Content, in.ToolName, argsJSON, resultJSON,
		in.TokenIn, in.TokenOut,
	)
	out, err := scanChatMessage(row)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// ListBySession returns the full transcript oldest-first. Used by the UI
// to render the bubble list and by the prompt builder when the session
// is short enough to send verbatim (≤ ChatTranscriptKeepLast).
func (s *ChatMessageStore) ListBySession(
	ctx context.Context, sessionID string,
) ([]domain.ChatMessage, error) {
	const q = `SELECT ` + chatMessageColumns + `
	             FROM chat_messages
	            WHERE session_id = $1
	         ORDER BY seq ASC`
	rows, err := s.DB.Query(ctx, q, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ChatMessage, 0)
	for rows.Next() {
		m, err := scanChatMessageRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// LastNForLLM returns up to N most-recent messages oldest-first — the
// shape the prompt builder wants. When the session has more than N
// messages the prompt builder will compose a rolling summary of the
// dropped prefix; that's the caller's job, not ours.
func (s *ChatMessageStore) LastNForLLM(
	ctx context.Context, sessionID string, n int,
) ([]domain.ChatMessage, error) {
	if n <= 0 {
		n = 30
	}
	// Subquery selects newest-first; outer query reverses for chronology.
	const q = `
		SELECT ` + chatMessageColumns + `
		  FROM (
		    SELECT id, session_id, seq, role, content,
		           tool_name, tool_args, tool_result, token_in, token_out, created_at
		      FROM chat_messages
		     WHERE session_id = $1
		     ORDER BY seq DESC
		     LIMIT $2
		  ) t
	  ORDER BY seq ASC`
	rows, err := s.DB.Query(ctx, q, sessionID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ChatMessage, 0, n)
	for rows.Next() {
		m, err := scanChatMessageRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// CountTurns returns the number of user+assistant rows for the session.
// Used by the streaming handler to gate against ChatMaxTurns; tool +
// system_event rows aren't counted because they're not user-driven.
func (s *ChatMessageStore) CountTurns(ctx context.Context, sessionID string) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx,
		`SELECT COUNT(*) FROM chat_messages
		  WHERE session_id = $1
		    AND role IN ('user','assistant')`,
		sessionID,
	).Scan(&n)
	return n, err
}

func scanChatMessage(row pgx.Row) (*domain.ChatMessage, error) {
	var m domain.ChatMessage
	var argsJSON, resultJSON []byte
	if err := row.Scan(
		&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content,
		&m.ToolName, &argsJSON, &resultJSON, &m.TokenIn, &m.TokenOut, &m.CreatedAt,
	); err != nil {
		return nil, err
	}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &m.ToolArgs); err != nil {
			return nil, fmt.Errorf("unmarshal tool_args: %w", err)
		}
	}
	if len(resultJSON) > 0 {
		if err := json.Unmarshal(resultJSON, &m.ToolResult); err != nil {
			return nil, fmt.Errorf("unmarshal tool_result: %w", err)
		}
	}
	return &m, nil
}

// scanChatMessageRows is a pgx.Rows-shaped variant for iteration.
func scanChatMessageRows(rows pgx.Rows) (*domain.ChatMessage, error) {
	var m domain.ChatMessage
	var argsJSON, resultJSON []byte
	if err := rows.Scan(
		&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content,
		&m.ToolName, &argsJSON, &resultJSON, &m.TokenIn, &m.TokenOut, &m.CreatedAt,
	); err != nil {
		return nil, err
	}
	if len(argsJSON) > 0 {
		if err := json.Unmarshal(argsJSON, &m.ToolArgs); err != nil {
			return nil, fmt.Errorf("unmarshal tool_args: %w", err)
		}
	}
	if len(resultJSON) > 0 {
		if err := json.Unmarshal(resultJSON, &m.ToolResult); err != nil {
			return nil, fmt.Errorf("unmarshal tool_result: %w", err)
		}
	}
	return &m, nil
}

func isValidChatRole(role string) bool {
	switch role {
	case domain.ChatRoleUser, domain.ChatRoleAssistant,
		domain.ChatRoleTool, domain.ChatRoleSystemEvent:
		return true
	}
	return false
}

// ensureNotNil is a small helper to make linter happy on unused errors var
// in older Go versions; kept simple for readability.
var _ = errors.New
