package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// SummaryHandler hosts /api/summaries/*. Reads come straight from the
// `summaries` table; the regenerate endpoint flips a row in
// `summary_jobs` back to pending so the worker picks it up next tick.
//
// Stats reads come from `daily_inputs` (the user-provided check-in)
// rather than `summaries.metadata` — that way the mood line + emotion
// bars reflect whatever the user just typed, without waiting for the
// next morning's daily summary to fire.
type SummaryHandler struct {
	Summaries   *store.SummaryStore
	Jobs        *store.SummaryJobStore
	Users       *store.UserStore
	DailyInputs *store.DailyInputStore
	Logger      *slog.Logger
}

const (
	defaultSummaryListLimit = 60
	maxSummaryListLimit     = 365
	defaultStatsWindowDays  = 90
	maxStatsWindowDays      = 365
)

// List handles GET /api/summaries?period=day&limit=N. Period filter is
// required — the four tabs each fetch their own list, mixing periods in
// one response would be ambiguous to render.
func (h *SummaryHandler) List(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	period := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period")))
	if !domain.IsValidPeriod(period) {
		writeJSONError(w, http.StatusBadRequest, "period must be day|week|month|year")
		return
	}
	limit := defaultSummaryListLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxSummaryListLimit {
			writeJSONError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	rows, err := h.Summaries.ListByPeriodType(r.Context(), sess.UserID, period, limit)
	if err != nil {
		h.Logger.Error("list summaries", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"period":    period,
		"summaries": rows,
	})
}

// Get handles GET /api/summaries/:id. 404 when the id isn't the caller's.
func (h *SummaryHandler) Get(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	row, err := h.Summaries.GetByID(r.Context(), sess.UserID, id)
	if err != nil {
		if errors.Is(err, store.ErrSummaryNotFound) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		h.Logger.Error("get summary", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "get failed")
		return
	}
	writeJSON(w, http.StatusOK, row)
}

type regenerateRequest struct {
	PeriodType  string `json:"period_type"`
	PeriodStart string `json:"period_start"`
}

// Regenerate handles POST /api/summaries/regenerate. Takes
// {period_type, period_start} and resets a summary_jobs row so the
// dispatcher picks it up next tick. Returns 202 Accepted because the
// LLM call is async — the SummariesPage will see the new row appear
// after a refetch (TanStack Query polls or after the user navigates
// back to the page).
//
// Idempotent at the (user, period_type, period_start) granularity:
// clicking twice within a tick is a no-op for the second click.
func (h *SummaryHandler) Regenerate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req regenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if !domain.IsValidPeriod(req.PeriodType) {
		writeJSONError(w, http.StatusBadRequest, "invalid period_type")
		return
	}
	periodStart, err := time.Parse("2006-01-02", req.PeriodStart)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid period_start")
		return
	}

	user, err := h.Users.GetByID(r.Context(), sess.UserID)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusUnauthorized, "user not found")
		return
	}

	// PeriodFromLocalStart: stored period_starts are already canonical;
	// PeriodContaining would re-apply the day_start shift.
	period, err := timezone.PeriodFromLocalStart(
		periodStart, user.Timezone, user.DayStartMinutes,
		domain.SummaryPeriod(req.PeriodType),
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Use period.Start (canonical for this period_type) — guards against
	// callers passing e.g. a Tuesday for a weekly period.
	triggered, err := h.Jobs.ResetForRegeneration(
		r.Context(), sess.UserID, req.PeriodType, period.Start, time.Now(),
	)
	if err != nil {
		h.Logger.Error("regenerate", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "regenerate failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"triggered":    triggered,
		"period_type":  req.PeriodType,
		"period_start": timezone.FormatDate(period.Start),
	})
}

// Stats handles GET /api/summaries/stats?days=N. Returns the panel data
// for SummariesPage: mood sparkline, top emotions, current streak, total
// entries in window. Window defaults to 90 days, capped at 365.
func (h *SummaryHandler) Stats(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	days := defaultStatsWindowDays
	if s := r.URL.Query().Get("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxStatsWindowDays {
			writeJSONError(w, http.StatusBadRequest, "invalid days")
			return
		}
		days = n
	}

	mood, err := h.DailyInputs.MoodSeries(r.Context(), sess.UserID, days)
	if err != nil {
		h.Logger.Error("stats: mood", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "stats failed")
		return
	}
	emotions, err := h.DailyInputs.TopEmotions(r.Context(), sess.UserID, days, 6)
	if err != nil {
		h.Logger.Error("stats: emotions", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "stats failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"window_days": days,
		"mood":        mood,
		"emotions":    emotions,
	})
}
