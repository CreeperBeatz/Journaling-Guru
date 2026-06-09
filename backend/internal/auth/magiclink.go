package auth

import (
	"context"
	"net/netip"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/store"
)

// MagicLinkConfig captures the policy knobs that govern issue/verify. All
// values are required (defaults live in config package).
type MagicLinkConfig struct {
	TTL         time.Duration
	PerEmail15m int
	PerEmailDay int
	PerIPHour   int
}

// MagicLinkService issues and verifies one-time login links. It carries no
// per-request state — wire it once at boot and call from handlers.
type MagicLinkService struct {
	Cfg   MagicLinkConfig
	Users *store.UserStore
	Links *store.MagicLinkStore
}

func NewMagicLinkService(cfg MagicLinkConfig, users *store.UserStore, links *store.MagicLinkStore) *MagicLinkService {
	return &MagicLinkService{Cfg: cfg, Users: users, Links: links}
}

// IssueResult is what handlers need from a successful issue: the raw token
// (to embed in the email URL) and the email address it's bound to.
type IssueResult struct {
	UserID    string
	Email     string
	RawToken  string
	ExpiresAt time.Time
}

// Issue applies rate limits, ensures a user row exists, and writes a fresh
// hashed token. Returns ErrRateLimited when any cap is exceeded; the caller
// should still respond 200/429 in a way that doesn't leak account existence.
//
// Order of checks: IP first (cheap, no user touch), then UPSERT, then
// per-user windows. This way an attacker can't farm user rows by sending
// magic links for many addresses from one IP.
func (s *MagicLinkService) Issue(
	ctx context.Context,
	email string,
	ip netip.Addr,
	userAgent string,
) (*IssueResult, error) {
	now := time.Now()

	if s.Cfg.PerIPHour > 0 {
		n, err := s.Links.CountForIPSince(ctx, ip, now.Add(-time.Hour))
		if err != nil {
			return nil, err
		}
		if n >= s.Cfg.PerIPHour {
			return nil, ErrRateLimited
		}
	}

	user, err := s.Users.UpsertByEmail(ctx, email)
	if err != nil {
		return nil, err
	}

	if s.Cfg.PerEmail15m > 0 {
		n, err := s.Links.CountForUserSince(ctx, user.ID, now.Add(-15*time.Minute))
		if err != nil {
			return nil, err
		}
		if n >= s.Cfg.PerEmail15m {
			return nil, ErrRateLimited
		}
	}
	if s.Cfg.PerEmailDay > 0 {
		n, err := s.Links.CountForUserSince(ctx, user.ID, now.Add(-24*time.Hour))
		if err != nil {
			return nil, err
		}
		if n >= s.Cfg.PerEmailDay {
			return nil, ErrRateLimited
		}
	}

	raw, hash, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	expires := now.Add(s.Cfg.TTL)
	if err := s.Links.Insert(ctx, user.ID, hash, expires, ip, userAgent); err != nil {
		return nil, err
	}
	return &IssueResult{
		UserID:    user.ID,
		Email:     user.Email,
		RawToken:  raw,
		ExpiresAt: expires,
	}, nil
}

// Verify consumes a magic-link token atomically and returns the user it was
// bound to. ErrInvalidToken on any miss (unknown token, already consumed,
// expired) — the caller should not distinguish, to avoid token oracle leaks.
//
// Post-consume, email_verified is flipped (idempotent).
func (s *MagicLinkService) Verify(ctx context.Context, rawToken string) (string, error) {
	if rawToken == "" {
		return "", ErrInvalidToken
	}
	hash := HashToken(rawToken)
	userID, err := s.Links.ConsumeByHash(ctx, hash)
	if err != nil {
		return "", ErrInvalidToken
	}
	if err := s.Users.MarkEmailVerified(ctx, userID); err != nil {
		return "", err
	}
	return userID, nil
}
