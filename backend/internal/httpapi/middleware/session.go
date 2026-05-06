package middleware

import (
	"context"
	"errors"
	"net/http"

	"github.com/cosmosthrace/journai/backend/internal/auth"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

type sessionCtxKey int

const sessionKey sessionCtxKey = 0

// SessionFromCtx returns the resolved session attached by RequireAuth /
// OptionalAuth, or nil if the request was not authenticated.
func SessionFromCtx(ctx context.Context) *store.Session {
	if s, ok := ctx.Value(sessionKey).(*store.Session); ok {
		return s
	}
	return nil
}

// RequireAuth resolves the session cookie and rejects with 401 when missing
// or invalid. Routes that mutate user data must sit behind this; the CSRF
// middleware composes on top.
func RequireAuth(svc *auth.SessionService, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := readSessionCookie(r, cookieName)
			sess, err := svc.Resolve(r.Context(), raw)
			if err != nil {
				if errors.Is(err, auth.ErrNoSession) {
					writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
					return
				}
				writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
				return
			}
			ctx := context.WithValue(r.Context(), sessionKey, sess)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth attaches the session if present, but doesn't reject when
// it's missing. Used by routes like /api/me that distinguish 401 vs
// returning {user: null}.
func OptionalAuth(svc *auth.SessionService, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := readSessionCookie(r, cookieName)
			if raw != "" {
				if sess, err := svc.Resolve(r.Context(), raw); err == nil && sess != nil {
					ctx := context.WithValue(r.Context(), sessionKey, sess)
					r = r.WithContext(ctx)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func readSessionCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
