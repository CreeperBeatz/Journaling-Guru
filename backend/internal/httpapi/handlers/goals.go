package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// GoalHandler hosts /api/goals/*. Goals close the loop between "spotted
// a pattern" and "tried to change something." Each active goal renders
// a yes/no daily check-in on /today.
type GoalHandler struct {
	Goals    *store.GoalStore
	CheckIns *store.GoalCheckInStore
	Users    *store.UserStore
	Logger   *slog.Logger
}

const (
	maxGoalTitleLen           = 200
	maxGoalCheckInQuestionLen = 200
	maxGoalConclusionLen      = 1_000
	maxGoalDurationDays       = 366 // spec says "indefinite" upper bound, but a year keeps wrap-up sane
)

func (h *GoalHandler) resolveToday(r *http.Request, userID string) (time.Time, error) {
	u, err := h.Users.GetByID(r.Context(), userID)
	if err != nil {
		return time.Time{}, err
	}
	if u == nil {
		return time.Time{}, errors.New("user not found")
	}
	return timezone.LocalDate(time.Now(), u.Timezone, u.DayStartMinutes)
}

// ListActive handles GET /api/goals?status=active|all. Default returns
// every goal (active + historical, newest-first) to drive Zone 3 of the
// summary page; ?status=active filters to today's check-in surface.
func (h *GoalHandler) List(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))

	if statusFilter == "active" {
		today, err := h.resolveToday(r, sess.UserID)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
			return
		}
		goals, err := h.Goals.ListActive(r.Context(), sess.UserID, today)
		if err != nil {
			h.Logger.Error("list active goals", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "list failed")
			return
		}
		// Pair with today's check-ins so the FE renders pre-filled
		// yes/no answers without a second round-trip.
		checkins, err := h.CheckIns.GetForDay(r.Context(), sess.UserID, today)
		if err != nil {
			h.Logger.Error("get day checkins", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "list failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"goals":             goals,
			"todays_check_ins":  checkins,
			"local_date":        timezone.FormatDate(today),
		})
		return
	}

	all, err := h.Goals.ListAll(r.Context(), sess.UserID)
	if err != nil {
		h.Logger.Error("list goals", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"goals": all})
}

type createGoalRequest struct {
	Title           string  `json:"title"`
	CheckInQuestion string  `json:"check_in_question"`
	EndDate         string  `json:"end_date"`   // YYYY-MM-DD
	StartDate       *string `json:"start_date"` // optional; defaults to today
}

// Create makes a new active goal. The SMART shaper (Phase 5) will
// pre-validate measurability before calling this; for now it accepts a
// pre-shaped title + check_in_question + end_date.
func (h *GoalHandler) Create(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req createGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" || len(title) > maxGoalTitleLen {
		writeJSONError(w, http.StatusBadRequest, "title required (1-200 chars)")
		return
	}
	question := strings.TrimSpace(req.CheckInQuestion)
	if question == "" || len(question) > maxGoalCheckInQuestionLen {
		writeJSONError(w, http.StatusBadRequest, "check_in_question required (1-200 chars)")
		return
	}
	endDate, err := time.Parse("2006-01-02", req.EndDate)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "end_date must be YYYY-MM-DD")
		return
	}

	today, err := h.resolveToday(r, sess.UserID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}
	startDate := today
	if req.StartDate != nil && strings.TrimSpace(*req.StartDate) != "" {
		parsed, err := time.Parse("2006-01-02", *req.StartDate)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "start_date must be YYYY-MM-DD")
			return
		}
		startDate = parsed
	}
	if endDate.Before(startDate) {
		writeJSONError(w, http.StatusBadRequest, "end_date must be on or after start_date")
		return
	}
	if endDate.Sub(startDate).Hours()/24 > maxGoalDurationDays {
		writeJSONError(w, http.StatusBadRequest, "goals capped at one year")
		return
	}

	goal, err := h.Goals.Create(r.Context(), sess.UserID, title, question, startDate, endDate)
	if err != nil {
		h.Logger.Error("create goal", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, goal)
}

type updateGoalRequest struct {
	Action          string `json:"action"`           // "complete" | "abandon"
	Outcome         string `json:"outcome"`          // for complete: "kept" | "dropped" | "inconclusive"
	ConclusionText  string `json:"conclusion_text"`
}

// Update handles PATCH /api/goals/:id — wraps up an active goal. Two
// flavors: action="complete" (with an outcome + optional why) or
// action="abandon" (optional why). Both flip status terminal; the goal
// row stays for Zone-3 history.
func (h *GoalHandler) Update(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := chi.URLParam(r, "id")
	var req updateGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.ConclusionText) > maxGoalConclusionLen {
		writeJSONError(w, http.StatusBadRequest, "conclusion text too long")
		return
	}
	conclusion := strings.TrimSpace(req.ConclusionText)

	var (
		goal *domain.Goal
		err  error
	)
	switch strings.TrimSpace(req.Action) {
	case "complete":
		outcome := strings.TrimSpace(req.Outcome)
		switch outcome {
		case domain.GoalOutcomeKept, domain.GoalOutcomeDropped, domain.GoalOutcomeInconclusive:
		default:
			writeJSONError(w, http.StatusBadRequest, "outcome must be kept|dropped|inconclusive")
			return
		}
		goal, err = h.Goals.Complete(r.Context(), sess.UserID, id, outcome, conclusion)
	case "abandon":
		goal, err = h.Goals.Abandon(r.Context(), sess.UserID, id, conclusion)
	default:
		writeJSONError(w, http.StatusBadRequest, "action must be complete|abandon")
		return
	}
	if err != nil {
		h.Logger.Error("update goal", "err", err, "action", req.Action)
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}
	if goal == nil {
		writeJSONError(w, http.StatusNotFound, "goal not found or already ended")
		return
	}
	writeJSON(w, http.StatusOK, goal)
}

type checkInRequest struct {
	LocalDate *string `json:"local_date"` // optional; defaults to today
	Value     bool    `json:"value"`
}

// CheckIn handles POST /api/goals/:id/check-ins — yes/no answer for a
// goal on a given day. Defaults to today's local_date. Validates the
// goal belongs to the caller and is currently active; rejects dates
// outside the goal's date range so historical fills can't backdate
// arbitrarily.
func (h *GoalHandler) CheckIn(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := chi.URLParam(r, "id")
	var req checkInRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}

	goal, err := h.Goals.GetByID(r.Context(), sess.UserID, id)
	if err != nil {
		h.Logger.Error("get goal", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "check-in failed")
		return
	}
	if goal == nil {
		writeJSONError(w, http.StatusNotFound, "goal not found")
		return
	}
	if goal.Status != domain.GoalStatusActive {
		writeJSONError(w, http.StatusConflict, "goal is not active")
		return
	}

	date, err := h.resolveCheckInDate(r.Context(), sess.UserID, req.LocalDate)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	startDate, _ := time.Parse("2006-01-02", goal.StartDate)
	endDate, _ := time.Parse("2006-01-02", goal.EndDate)
	if date.Before(startDate) || date.After(endDate) {
		writeJSONError(w, http.StatusBadRequest, "date outside goal range")
		return
	}

	c, err := h.CheckIns.Upsert(r.Context(), id, date, req.Value)
	if err != nil {
		h.Logger.Error("upsert check-in", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "check-in failed")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (h *GoalHandler) resolveCheckInDate(ctx context.Context, userID string, raw *string) (time.Time, error) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		u, err := h.Users.GetByID(ctx, userID)
		if err != nil {
			return time.Time{}, err
		}
		if u == nil {
			return time.Time{}, errors.New("user not found")
		}
		return timezone.LocalDate(time.Now(), u.Timezone, u.DayStartMinutes)
	}
	d, err := time.Parse("2006-01-02", *raw)
	if err != nil {
		return time.Time{}, errors.New("local_date must be YYYY-MM-DD")
	}
	return d, nil
}
