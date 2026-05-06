package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/auth"
	mailpkg "github.com/cosmosthrace/journai/backend/internal/mail"
)

// AuthHandler hosts magic-link request, verify, and logout. Wired into
// /api/auth/* under the CSRF middleware.
type AuthHandler struct {
	Magic         *auth.MagicLinkService
	Sessions      *auth.SessionService
	Mailer        mailpkg.Mailer
	Logger        *slog.Logger
	PublicBaseURL string
	CookieName    string
	CookieSecure  bool
	CookieTTL     time.Duration
	MagicTTL      time.Duration
}

type magicLinkRequest struct {
	Email string `json:"email"`
}

type ackResponse struct {
	OK bool `json:"ok"`
}

// MagicLinkRequest accepts {email} and emails a one-time sign-in link.
//
// Always responds 200 on success or 429 on rate limit; 400 only for an
// outright malformed body or empty email. The 200/429 dichotomy is
// constant-time wrt user-existence to avoid an enumeration oracle.
func (h *AuthHandler) MagicLinkRequest(w http.ResponseWriter, r *http.Request) {
	var req magicLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" {
		writeJSONError(w, http.StatusBadRequest, "email required")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid email")
		return
	}

	res, err := h.Magic.Issue(r.Context(), email, clientIP(r), userAgent(r))
	if err != nil {
		if errors.Is(err, auth.ErrRateLimited) {
			writeJSONError(w, http.StatusTooManyRequests, "rate limited")
			return
		}
		h.Logger.Error("magic-link issue failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "could not send sign-in email")
		return
	}

	// Send email outside the request lifetime — the user already gets
	// their 200 even if the SMTP server is slow. Failures are logged so
	// we can correlate with mailhog or postmark dashboards.
	go func(to, raw string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		msg, err := mailpkg.BuildMagicLinkMessage(
			to, h.PublicBaseURL, raw,
			fmt.Sprintf("%d minutes", int(h.MagicTTL.Minutes())),
		)
		if err != nil {
			h.Logger.Error("build magic-link email", "err", err)
			return
		}
		if err := h.Mailer.Send(ctx, msg); err != nil {
			h.Logger.Error("send magic-link email", "err", err, "to", to)
			return
		}
		h.Logger.Info("magic link sent", "to", to)
	}(res.Email, res.RawToken)

	writeJSON(w, http.StatusOK, ackResponse{OK: true})
}

type verifyRequest struct {
	Token string `json:"token"`
}

type verifyResponse struct {
	UserID string `json:"user_id"`
}

// MagicLinkVerify consumes the token (single-use), mints a session, sets
// the cookie, and returns the user id. Token replay returns 401.
func (h *AuthHandler) MagicLinkVerify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	userID, err := h.Magic.Verify(r.Context(), req.Token)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	issued, err := h.Sessions.Issue(r.Context(), userID, clientIP(r), userAgent(r))
	if err != nil {
		h.Logger.Error("session issue", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session create failed")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     h.CookieName,
		Value:    issued.RawToken,
		Path:     "/",
		Expires:  issued.ExpiresAt,
		MaxAge:   int(h.CookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, verifyResponse{UserID: userID})
}

// Logout deletes the session row and clears the cookie. No-op (still 200)
// if the cookie is absent — clients call this defensively on every "sign
// out" click without checking session state first.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(h.CookieName); err == nil {
		_ = h.Sessions.Revoke(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     h.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, ackResponse{OK: true})
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
