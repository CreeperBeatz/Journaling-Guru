package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// MeHandler exposes /api/me. The session middleware should sit in front:
// RequireAuth for authenticated reads, OptionalAuth where {user: null} is
// a valid response.
type MeHandler struct {
	Users  *store.UserStore
	Logger *slog.Logger
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

type updateMeRequest struct {
	DisplayName     *string `json:"display_name,omitempty"`
	Timezone        *string `json:"timezone,omitempty"`
	ReminderTime    *string `json:"reminder_time,omitempty"`
	ReminderEnabled *bool   `json:"reminder_enabled,omitempty"`
	DayStartMinutes *int    `json:"day_start_minutes,omitempty"`
}

// reminderTimePattern matches "HH:MM" or "HH:MM:SS" with leading zeros.
// Postgres `time` accepts both, but we validate up-front so a mistyped
// value surfaces as 400 rather than as a SQL error.
var reminderTimePattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d(:[0-5]\d)?$`)

// Update handles PATCH /api/me — partial settings update. Only the
// caller's own row is touched (sess.UserID).
func (h *MeHandler) Update(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req updateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}

	patch := store.SettingsPatch{
		ReminderEnabled: req.ReminderEnabled,
	}
	if req.DisplayName != nil {
		trimmed := strings.TrimSpace(*req.DisplayName)
		if len(trimmed) > 200 {
			writeJSONError(w, http.StatusBadRequest, "display_name too long")
			return
		}
		patch.DisplayName = &trimmed
	}
	if req.Timezone != nil {
		tz := strings.TrimSpace(*req.Timezone)
		if !timezone.IsValidIANA(tz) {
			writeJSONError(w, http.StatusBadRequest, "invalid timezone")
			return
		}
		patch.Timezone = &tz
	}
	if req.ReminderTime != nil {
		t := strings.TrimSpace(*req.ReminderTime)
		if !reminderTimePattern.MatchString(t) {
			writeJSONError(w, http.StatusBadRequest, "invalid reminder_time")
			return
		}
		patch.ReminderTime = &t
	}
	if req.DayStartMinutes != nil {
		v := *req.DayStartMinutes
		if v < 0 || v >= 1440 {
			writeJSONError(w, http.StatusBadRequest, "invalid day_start_minutes")
			return
		}
		patch.DayStartMinutes = &v
	}

	u, err := h.Users.UpdateSettings(r.Context(), sess.UserID, patch)
	if err != nil {
		h.Logger.Error("update settings", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if u == nil {
		writeJSONError(w, http.StatusUnauthorized, "user not found")
		return
	}
	writeJSON(w, http.StatusOK, u)
}
