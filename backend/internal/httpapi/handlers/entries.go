package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// SummaryScheduler is the interface the entries handler uses to lazy-seed
// summary_jobs on each successful write. Phase 4's *jobs.Scheduler
// implements it; using an interface here keeps the httpapi package
// decoupled from River's transitive deps.
type SummaryScheduler interface {
	LazySeed(ctx context.Context, userID string, at time.Time) error
}

// EntryHandler hosts /api/entries. Today is computed server-side from the
// user's stored timezone — never from a date the client sends.
type EntryHandler struct {
	Entries           *store.EntryStore
	Users             *store.UserStore
	WeeklyReflections *store.WeeklyReflectionStore // optional; surfaces reflection week_ends in heatmap
	Logger            *slog.Logger
	Scheduler         SummaryScheduler // nil-safe: phase-1 callers (tests) can omit
}

const maxEntryBodyLen = 16_000

// resolveDate returns the calendar date for `param` (YYYY-MM-DD) or the
// user's "today" when `param` is empty / "today". Always interpreted in
// the user's stored IANA timezone + day_start_minutes offset so a 1am
// reflection still files under yesterday for users with a 06:00 cutoff.
func (h *EntryHandler) resolveDate(r *http.Request, userID, param string) (time.Time, error) {
	u, err := h.Users.GetByID(r.Context(), userID)
	if err != nil {
		return time.Time{}, err
	}
	if u == nil {
		return time.Time{}, errors.New("user not found")
	}
	switch strings.ToLower(strings.TrimSpace(param)) {
	case "", "today":
		return timezone.LocalDate(time.Now(), u.Timezone, u.DayStartMinutes)
	}
	d, err := time.Parse("2006-01-02", param)
	if err != nil {
		return time.Time{}, errors.New("invalid date")
	}
	return d, nil
}

// ListByDate handles GET /api/entries?date=YYYY-MM-DD (or omitted = today).
func (h *EntryHandler) ListByDate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	d, err := h.resolveDate(r, sess.UserID, r.URL.Query().Get("date"))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.Entries.ListByDate(r.Context(), sess.UserID, d)
	if err != nil {
		h.Logger.Error("list entries", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"local_date": timezone.FormatDate(d),
		"entries":    rows,
	})
}

// ListDates handles GET /api/entries/dates?limit=N — HistoryView uses this
// to render the list of days with entries.
func (h *EntryHandler) ListDates(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	limit := 0
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	rows, err := h.Entries.ListDates(r.Context(), sess.UserID, limit)
	if err != nil {
		h.Logger.Error("list entry dates", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dates": rows})
}

// Heatmap handles GET /api/history/heatmap?from=YYYY-MM-DD&to=YYYY-MM-DD.
// Both parameters are optional; defaults are last-365-days ending today
// (in the user's timezone). The FE caps the range at 366 days client-side
// but the server enforces a hard cap to keep the query bounded.
func (h *EntryHandler) Heatmap(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	u, err := h.Users.GetByID(r.Context(), sess.UserID)
	if err != nil || u == nil {
		writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	today, err := timezone.LocalDate(time.Now(), u.Timezone, u.DayStartMinutes)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "tz resolve failed")
		return
	}

	parseDate := func(s string, fallback time.Time) (time.Time, error) {
		s = strings.TrimSpace(s)
		if s == "" {
			return fallback, nil
		}
		return time.Parse("2006-01-02", s)
	}
	to, err := parseDate(r.URL.Query().Get("to"), today)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid to")
		return
	}
	from, err := parseDate(r.URL.Query().Get("from"), to.AddDate(0, 0, -364))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid from")
		return
	}
	if to.Before(from) {
		writeJSONError(w, http.StatusBadRequest, "to before from")
		return
	}
	// Hard cap to keep the query bounded — 2 years is more than the year
	// view ever asks for.
	if to.Sub(from) > 730*24*time.Hour {
		writeJSONError(w, http.StatusBadRequest, "range too wide")
		return
	}

	rows, err := h.Entries.HeatmapRange(r.Context(), sess.UserID, from, to)
	if err != nil {
		h.Logger.Error("heatmap", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "heatmap failed")
		return
	}

	// Mark which days have a *completed* weekly reflection so the FE
	// can paint a small badge on the heatmap. We key on week_end (the
	// day the user opened the reflection on) — that's the day clicking
	// the heatmap takes them to. Past in-progress rows are omitted to
	// avoid badging a half-finished reflection a user may have abandoned.
	reflectionDates := []string{}
	if h.WeeklyReflections != nil {
		// week_start range covering [from-6, to] — a reflection whose
		// week_end falls in [from, to] has week_start in [from-6, to-6],
		// but we use a slightly wider window for simplicity.
		wrs, _ := h.WeeklyReflections.ListInRange(r.Context(), sess.UserID,
			from.AddDate(0, 0, -6), to)
		for _, wr := range wrs {
			if wr.CompletedAt == nil {
				continue
			}
			if wr.WeekEnd >= timezone.FormatDate(from) && wr.WeekEnd <= timezone.FormatDate(to) {
				reflectionDates = append(reflectionDates, wr.WeekEnd)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"from":                     timezone.FormatDate(from),
		"to":                       timezone.FormatDate(to),
		"today":                    timezone.FormatDate(today),
		"days":                     rows,
		"weekly_reflection_dates":  reflectionDates,
	})
}

// ListByQuestion handles GET /api/questions/:id/entries. Used by the
// Summary → By Question timeline; returns every answer the user has
// logged against one question, newest first.
func (h *EntryHandler) ListByQuestion(w http.ResponseWriter, r *http.Request) {
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
	limit := 0
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			writeJSONError(w, http.StatusBadRequest, "invalid limit")
			return
		}
		limit = n
	}
	rows, err := h.Entries.ListByQuestion(r.Context(), sess.UserID, id, limit)
	if err != nil {
		if errors.Is(err, store.ErrEntryQuestionMissing) {
			writeJSONError(w, http.StatusNotFound, "question not found")
			return
		}
		h.Logger.Error("list by question", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": rows})
}

type upsertEntryRequest struct {
	QuestionID string `json:"question_id"`
	Body       string `json:"body"`
}

// Upsert handles PUT /api/entries — write today's answer for a question.
// Empty body deletes the row, so the UI can "clear" an answer with the
// same call. local_date is always today in the user's timezone; we never
// trust a client-provided date here (history is read-only via ListByDate).
func (h *EntryHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req upsertEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if strings.TrimSpace(req.QuestionID) == "" {
		writeJSONError(w, http.StatusBadRequest, "question_id required")
		return
	}
	if len(req.Body) > maxEntryBodyLen {
		writeJSONError(w, http.StatusBadRequest, "body too long")
		return
	}

	today, err := h.resolveDate(r, sess.UserID, "today")
	if err != nil {
		h.Logger.Error("resolve today", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}

	entry, mutated, err := h.Entries.Upsert(
		r.Context(), sess.UserID, req.QuestionID, today, req.Body, "text",
	)
	if err != nil {
		if errors.Is(err, store.ErrEntryQuestionMissing) {
			writeJSONError(w, http.StatusNotFound, "question not found")
			return
		}
		h.Logger.Error("upsert entry", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "save failed")
		return
	}

	// Lazy-seed summary jobs for the four periods this write lands in.
	// ON CONFLICT DO NOTHING absorbs duplicates so calling it on every
	// keystroke save is safe — it's only an INSERT per period when the
	// row is new. Skip on empty-body deletes (mutated=true, entry=nil)
	// because creating the entry was undone.
	if entry != nil && h.Scheduler != nil {
		if err := h.Scheduler.LazySeed(r.Context(), sess.UserID, time.Now()); err != nil {
			// Log only — schedule churn must never block the user save.
			h.Logger.Warn("lazy seed", "err", err, "user_id", sess.UserID)
		}
	}

	if entry == nil {
		// Empty body → delete path. Return a tiny ack so the client can
		// distinguish "saved" from "cleared".
		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":    mutated,
			"local_date": timezone.FormatDate(today),
		})
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

type updateEntryRequest struct {
	Body string `json:"body"`
}

// UpdateByID handles PATCH /api/entries/:id — edit an existing entry's
// body without changing its local_date. The entry's date is fixed at
// creation time, so HistoryView can edit any past day without needing a
// trusted-date input from the client.
//
// Empty body deletes the row (consistent with Upsert).
func (h *EntryHandler) UpdateByID(w http.ResponseWriter, r *http.Request) {
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
	var req updateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Body) > maxEntryBodyLen {
		writeJSONError(w, http.StatusBadRequest, "body too long")
		return
	}

	entry, _, err := h.Entries.UpdateBody(r.Context(), sess.UserID, id, req.Body)
	if err != nil {
		if errors.Is(err, store.ErrEntryNotFound) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		h.Logger.Error("update entry", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if entry == nil {
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
		return
	}
	writeJSON(w, http.StatusOK, entry)
}
