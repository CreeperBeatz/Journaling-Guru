package handlers

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// AccountHandler owns destructive account-level operations. Phase 2 ships
// the soft-delete: marks the user deleted_at and clears all sessions and
// outstanding magic links so existing cookies stop working immediately.
//
// The full cascade across journal_entries / summaries / push_subscriptions
// lands in Phase 7 hardening, where we also pair this with a cron-driven
// hard-delete after a grace period.
type AccountHandler struct {
	Users        *store.UserStore
	Logger       *slog.Logger
	CookieName   string
	CookieSecure bool
}

// Delete tears down the current account (soft-delete) and clears the
// session cookie on the way out.
func (h *AccountHandler) Delete(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if err := h.Users.SoftDelete(r.Context(), sess.UserID); err != nil {
		h.Logger.Error("account soft-delete", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     h.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   h.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, ackResponse{OK: true})
}
