package auth

import "errors"

var (
	// ErrRateLimited — caller exceeded the per-email or per-IP magic-link cap.
	ErrRateLimited = errors.New("auth: rate limited")
	// ErrInvalidToken — magic-link or session token does not match an active row.
	ErrInvalidToken = errors.New("auth: invalid or expired token")
	// ErrNoSession — no session cookie or it pointed at a deleted/expired session.
	ErrNoSession = errors.New("auth: no active session")
)
