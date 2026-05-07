package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PushSubscription is the projection of one push_subscriptions row that
// the worker fans out to and the api returns to the client. p256dh and
// auth are kept on the read path only because the worker needs them at
// fan-out time — the FE never re-receives them.
type PushSubscription struct {
	ID          string    `json:"id"`
	UserID      string    `json:"-"`
	Endpoint    string    `json:"endpoint"`
	P256dh      string    `json:"-"`
	Auth        string    `json:"-"`
	UserAgent   *string   `json:"user_agent,omitempty"`
	LastUsedAt  time.Time `json:"last_used_at"`
	FailedCount int       `json:"failed_count"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PushSubscriptionStore struct {
	DB *pgxpool.Pool
}

func NewPushSubscriptionStore(db *pgxpool.Pool) *PushSubscriptionStore {
	return &PushSubscriptionStore{DB: db}
}

const pushSubscriptionColumns = `id, user_id, endpoint, p256dh, auth,
    user_agent, last_used_at, failed_count, created_at, updated_at`

func scanPushSubscription(row pgx.Row) (*PushSubscription, error) {
	var s PushSubscription
	if err := row.Scan(
		&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth,
		&s.UserAgent, &s.LastUsedAt, &s.FailedCount, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &s, nil
}

// Upsert binds (endpoint) to userID, refreshing keys + user_agent and
// resetting failed_count. The endpoint URL is globally unique across
// push services, so a re-subscribe under a different account
// transfers ownership rather than producing a duplicate row.
//
// Returning the row lets the caller surface the canonical id back to
// the client without a second round-trip.
func (s *PushSubscriptionStore) Upsert(
	ctx context.Context,
	userID, endpoint, p256dh, auth string,
	userAgent *string,
) (*PushSubscription, error) {
	const q = `
		INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (endpoint) DO UPDATE
		   SET user_id      = EXCLUDED.user_id,
		       p256dh       = EXCLUDED.p256dh,
		       auth         = EXCLUDED.auth,
		       user_agent   = EXCLUDED.user_agent,
		       last_used_at = now(),
		       failed_count = 0,
		       updated_at   = now()
		RETURNING ` + pushSubscriptionColumns
	row := s.DB.QueryRow(ctx, q, userID, endpoint, p256dh, auth, userAgent)
	return scanPushSubscription(row)
}

// ListByUser returns the user's active subscriptions in newest-first
// order. Used by the worker for fan-out and by the api for the
// "currently subscribed devices" list.
func (s *PushSubscriptionStore) ListByUser(ctx context.Context, userID string) ([]PushSubscription, error) {
	const q = `SELECT ` + pushSubscriptionColumns + `
	             FROM push_subscriptions
	            WHERE user_id = $1
	            ORDER BY last_used_at DESC`
	rows, err := s.DB.Query(ctx, q, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]PushSubscription, 0)
	for rows.Next() {
		s, err := scanPushSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// CountByUser is the cheap presence check the api uses on /me/state to
// decide whether to render the subscribe button or the "subscribed on
// N devices" badge — we don't ship raw endpoints to the client.
func (s *PushSubscriptionStore) CountByUser(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.DB.QueryRow(ctx, `SELECT count(*) FROM push_subscriptions WHERE user_id = $1`, userID).Scan(&n)
	return n, err
}

// DeleteByEndpoint removes one subscription. User-scoped on the api
// path (so unsubscribing from another tenant's row is impossible);
// the worker uses DeleteByID after a 410 Gone with no scoping needed.
func (s *PushSubscriptionStore) DeleteByEndpoint(ctx context.Context, userID, endpoint string) error {
	_, err := s.DB.Exec(ctx,
		`DELETE FROM push_subscriptions
		   WHERE user_id = $1 AND endpoint = $2`,
		userID, endpoint)
	return err
}

// DeleteByID is the worker's terminal-failure path. No user scoping —
// the worker is trusted code, and the id was just produced by ListByUser.
func (s *PushSubscriptionStore) DeleteByID(ctx context.Context, id string) error {
	_, err := s.DB.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
	return err
}

// IncrementFailure bumps failed_count. The worker calls DeleteByID
// itself once the count crosses the threshold — the store doesn't
// know the policy, only how to keep score.
func (s *PushSubscriptionStore) IncrementFailure(ctx context.Context, id string) (int, error) {
	const q = `
		UPDATE push_subscriptions
		   SET failed_count = failed_count + 1,
		       updated_at   = now()
		 WHERE id = $1
		 RETURNING failed_count`
	var n int
	err := s.DB.QueryRow(ctx, q, id).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// MarkSuccess records a delivery: bump last_used_at, clear failed_count.
// Best-effort — the worker won't surface errors here because they don't
// affect message delivery, only the next fan-out's accuracy.
func (s *PushSubscriptionStore) MarkSuccess(ctx context.Context, id string) {
	_, _ = s.DB.Exec(ctx,
		`UPDATE push_subscriptions
		    SET last_used_at = now(), failed_count = 0, updated_at = now()
		  WHERE id = $1`, id)
}
