package store

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Session is the projection of `sessions` we hand back to middleware: just
// what's needed to identify the user and decide whether the row is still
// valid.
type Session struct {
	ID         string
	UserID     string
	ExpiresAt  time.Time
	LastSeenAt time.Time
}

type SessionStore struct {
	DB *pgxpool.Pool
}

func NewSessionStore(db *pgxpool.Pool) *SessionStore { return &SessionStore{DB: db} }

// Insert creates a new session row for `userID`. token_hash collisions are
// astronomically unlikely with 32 bytes of crypto/rand; we let the UNIQUE
// constraint surface them as a server error rather than retrying.
func (s *SessionStore) Insert(
	ctx context.Context,
	userID string,
	tokenHash []byte,
	expiresAt time.Time,
	ip netip.Addr,
	userAgent string,
) (string, error) {
	const q = `
		INSERT INTO sessions (user_id, token_hash, expires_at, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id`
	var ipArg interface{}
	if ip.IsValid() {
		ipArg = ip.String()
	}
	var id string
	err := s.DB.QueryRow(ctx, q, userID, tokenHash, expiresAt, ipArg, userAgent).Scan(&id)
	return id, err
}

// GetByHash returns the session matching the cookie token, or pgx.ErrNoRows
// if absent / expired. Joined to users so we can reject sessions for soft-
// deleted accounts in one round-trip.
func (s *SessionStore) GetByHash(ctx context.Context, tokenHash []byte) (*Session, error) {
	const q = `
		SELECT s.id, s.user_id, s.expires_at, s.last_seen_at
		  FROM sessions s
		  JOIN users u ON u.id = s.user_id
		 WHERE s.token_hash = $1
		   AND s.expires_at > now()
		   AND u.deleted_at IS NULL`
	var sess Session
	err := s.DB.QueryRow(ctx, q, tokenHash).Scan(
		&sess.ID, &sess.UserID, &sess.ExpiresAt, &sess.LastSeenAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// TouchLastSeen bumps last_seen_at to now(). Best-effort: failures are not
// surfaced because they don't affect auth correctness.
func (s *SessionStore) TouchLastSeen(ctx context.Context, id string) {
	_, _ = s.DB.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, id)
}

// DeleteByHash removes a single session row (logout).
func (s *SessionStore) DeleteByHash(ctx context.Context, tokenHash []byte) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	return err
}
