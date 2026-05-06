package auth

import (
	"context"
	"net/netip"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/store"
)

// SessionConfig pins the cookie name + TTL for issued sessions; CookieSecure
// flips the Secure flag on prod cookies.
type SessionConfig struct {
	CookieName   string
	TTL          time.Duration
	CookieSecure bool
}

// SessionService mints session cookies and resolves cookie → user. Only the
// hash is stored; the raw token rides on the cookie.
type SessionService struct {
	Cfg      SessionConfig
	Sessions *store.SessionStore
}

func NewSessionService(cfg SessionConfig, sessions *store.SessionStore) *SessionService {
	return &SessionService{Cfg: cfg, Sessions: sessions}
}

// Issued is what the handler needs to set the cookie: raw token + the
// expiry we already wrote to the DB row.
type Issued struct {
	RawToken  string
	ExpiresAt time.Time
}

// Issue mints a new session for `userID`, persists the hash, and returns the
// raw cookie token + expiry. Cookie writing is the handler's job.
func (s *SessionService) Issue(
	ctx context.Context,
	userID string,
	ip netip.Addr,
	userAgent string,
) (*Issued, error) {
	raw, hash, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	expires := time.Now().Add(s.Cfg.TTL)
	if _, err := s.Sessions.Insert(ctx, userID, hash, expires, ip, userAgent); err != nil {
		return nil, err
	}
	return &Issued{RawToken: raw, ExpiresAt: expires}, nil
}

// Resolve returns the session matching `rawToken`, or ErrNoSession if the
// cookie is unknown / expired / belongs to a deleted user.
func (s *SessionService) Resolve(ctx context.Context, rawToken string) (*store.Session, error) {
	if rawToken == "" {
		return nil, ErrNoSession
	}
	sess, err := s.Sessions.GetByHash(ctx, HashToken(rawToken))
	if err != nil {
		return nil, err
	}
	if sess == nil {
		return nil, ErrNoSession
	}
	go s.Sessions.TouchLastSeen(context.Background(), sess.ID)
	return sess, nil
}

// Revoke deletes the session row for `rawToken` (logout).
func (s *SessionService) Revoke(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	return s.Sessions.DeleteByHash(ctx, HashToken(rawToken))
}
