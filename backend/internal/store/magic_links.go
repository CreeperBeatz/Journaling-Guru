package store

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MagicLinkStore handles `magic_link_tokens` rows: insert at request, atomic
// single-use consume on verify, and the count queries that drive rate limits.
type MagicLinkStore struct {
	DB *pgxpool.Pool
}

func NewMagicLinkStore(db *pgxpool.Pool) *MagicLinkStore { return &MagicLinkStore{DB: db} }

// Insert stores a new (hashed) token bound to a user. ip and userAgent are
// audited for rate-limit and forensic queries; either can be empty.
func (s *MagicLinkStore) Insert(
	ctx context.Context,
	userID string,
	tokenHash []byte,
	expiresAt time.Time,
	ip netip.Addr,
	userAgent string,
) error {
	const q = `
		INSERT INTO magic_link_tokens
		    (user_id, token_hash, expires_at, ip_address, user_agent)
		VALUES ($1, $2, $3, $4, $5)`
	var ipArg interface{}
	if ip.IsValid() {
		ipArg = ip.String()
	}
	_, err := s.DB.Exec(ctx, q, userID, tokenHash, expiresAt, ipArg, userAgent)
	return err
}

// ConsumeByHash atomically marks the token consumed and returns its user_id.
// Returns pgx.ErrNoRows if the token is unknown, already used, or expired.
func (s *MagicLinkStore) ConsumeByHash(ctx context.Context, tokenHash []byte) (userID string, err error) {
	const q = `
		UPDATE magic_link_tokens
		   SET consumed_at = now()
		 WHERE token_hash = $1
		   AND consumed_at IS NULL
		   AND expires_at > now()
		RETURNING user_id`
	err = s.DB.QueryRow(ctx, q, tokenHash).Scan(&userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	return userID, err
}

// CountForUserSince counts tokens issued for `userID` within the window. The
// magic_link_tokens (user_id, created_at DESC) index handles this in O(log n).
func (s *MagicLinkStore) CountForUserSince(ctx context.Context, userID string, since time.Time) (int, error) {
	const q = `SELECT count(*) FROM magic_link_tokens
	            WHERE user_id = $1 AND created_at >= $2`
	var n int
	err := s.DB.QueryRow(ctx, q, userID, since).Scan(&n)
	return n, err
}

// CountForIPSince counts tokens issued from `ip` within the window.
func (s *MagicLinkStore) CountForIPSince(ctx context.Context, ip netip.Addr, since time.Time) (int, error) {
	if !ip.IsValid() {
		return 0, nil
	}
	const q = `SELECT count(*) FROM magic_link_tokens
	            WHERE ip_address = $1 AND created_at >= $2`
	var n int
	err := s.DB.QueryRow(ctx, q, ip.String(), since).Scan(&n)
	return n, err
}
