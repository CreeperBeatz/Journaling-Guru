package handlers

import (
	"net/http"

	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// MeHandler exposes /api/me. The session middleware should sit in front:
// RequireAuth for authenticated reads, OptionalAuth where {user: null} is
// a valid response.
type MeHandler struct {
	Users *store.UserStore
}

// Get returns the current user. Returns 401 when no session is attached
// (which RequireAuth prevents from happening, but we double-check so this
// handler is safe to mount under OptionalAuth too).
func (h *MeHandler) Get(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	u, err := h.Users.GetByID(r.Context(), sess.UserID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	if u == nil {
		writeJSONError(w, http.StatusUnauthorized, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}
