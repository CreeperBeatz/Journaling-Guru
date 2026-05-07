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

	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// EmotionClassifyScheduler is the queue interface the handler uses to
// arm the async Plutchik classifier on every save. Implemented by
// *store.EmotionClassifyJobStore. Wrapped behind an interface so the
// handler tests can pass a fake.
type EmotionClassifyScheduler interface {
	Schedule(ctx context.Context, userID string, localDate time.Time, fireAt time.Time) (bool, error)
}

// DailyInputHandler hosts /api/daily/inputs/*. The check-in surface for
// mood, emotions, and notes — paralleling the journal_entries handlers
// but per-day instead of per-question.
type DailyInputHandler struct {
	Inputs      *store.DailyInputStore
	Users       *store.UserStore
	Logger      *slog.Logger
	Scheduler   SummaryScheduler         // shares the interface with EntryHandler — same lazy-seed contract
	EmotionJobs EmotionClassifyScheduler // armed on every save with non-empty emotions_text
}

const (
	maxNotesLen         = 4_000
	maxEmotionsTextLen  = 1_000
)

// resolveDate is the same convention used by EntryHandler: empty/"today"
// resolves server-side via timezone.LocalDate; a YYYY-MM-DD string is
// parsed verbatim. Past dates are still per-tenant scoped at the
// store layer.
func (h *DailyInputHandler) resolveDate(r *http.Request, userID, param string) (time.Time, error) {
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

// Get handles GET /api/daily/inputs?date=YYYY-MM-DD (or omitted = today).
// Returns 200 with the row, or 200 with `{input: null, local_date: ...}`
// when nothing has been logged for the day yet — DailyInputs UI uses
// the null to render the empty state.
func (h *DailyInputHandler) Get(w http.ResponseWriter, r *http.Request) {
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
	row, err := h.Inputs.GetByDate(r.Context(), sess.UserID, d)
	if err != nil {
		h.Logger.Error("get daily inputs", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "get failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"local_date": timezone.FormatDate(d),
		"input":      row, // nil when no row exists yet
	})
}

type upsertDailyInputRequest struct {
	MoodScore    *int   `json:"mood_score"`
	EmotionsText string `json:"emotions_text"`
	Notes        string `json:"notes"`
}

// Upsert handles PUT /api/daily/inputs — write today's check-in.
// Empty (mood=null, emotions_text="", notes="") deletes the row,
// matching the journal_entries empty-deletes convention.
func (h *DailyInputHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	req, err := decodeUpsert(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	today, err := h.resolveDate(r, sess.UserID, "today")
	if err != nil {
		h.Logger.Error("resolve today", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}
	h.write(r.Context(), w, sess.UserID, today, req, true)
}

// UpdateByDate handles PATCH /api/daily/inputs/by-date/:date — past-day
// edits. Mirrors EntryHandler.UpdateByID semantics: HistoryView uses
// this to amend yesterday's mood after the day has rolled over.
func (h *DailyInputHandler) UpdateByDate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	dateParam := chi.URLParam(r, "date")
	d, err := time.Parse("2006-01-02", dateParam)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid date")
		return
	}
	req, err := decodeUpsert(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Past edits don't lazy-seed — the summary_jobs row for that day
	// already exists (or was scheduled when entries were first
	// written). Re-seeding would only insert ON CONFLICT-NOTHING.
	h.write(r.Context(), w, sess.UserID, d, req, false)
}

func (h *DailyInputHandler) write(
	ctx context.Context, w http.ResponseWriter,
	userID string, localDate time.Time,
	req *upsertDailyInputRequest, lazySeed bool,
) {
	row, mutated, err := h.Inputs.Upsert(
		ctx, userID, localDate,
		req.MoodScore, req.EmotionsText, req.Notes,
	)
	if err != nil {
		h.Logger.Error("upsert daily input", "err", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "save failed")
		return
	}
	// Lazy-seed: a "just notes + mood" day still produces a daily
	// summary, so the worker should know to fire even if the user
	// never answered any questions.
	if lazySeed && row != nil && h.Scheduler != nil {
		if err := h.Scheduler.LazySeed(ctx, userID, time.Now()); err != nil {
			h.Logger.Warn("lazy seed (daily input)", "err", err, "user_id", userID)
		}
	}
	// Arm the Plutchik classifier whenever emotions_text has content.
	// When the user clears the text, write an empty classified_emotions
	// synchronously so SummaryDetail/EmotionBars stop showing stale
	// pills before the worker would have run. (Skipping this would let
	// the previous classification linger forever for that day.)
	if h.EmotionJobs != nil {
		if row != nil && strings.TrimSpace(row.EmotionsText) != "" {
			if _, err := h.EmotionJobs.Schedule(ctx, userID, localDate, time.Now()); err != nil {
				h.Logger.Warn("schedule emotion classify", "err", err, "user_id", userID)
			}
		} else {
			if err := h.Inputs.WriteClassifiedEmotions(ctx, userID, localDate, nil); err != nil {
				h.Logger.Warn("clear classified_emotions", "err", err, "user_id", userID)
			}
		}
	}
	if row == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":    mutated,
			"local_date": timezone.FormatDate(localDate),
		})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func decodeUpsert(r *http.Request) (*upsertDailyInputRequest, error) {
	var req upsertDailyInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.New("invalid json")
	}
	if req.MoodScore != nil {
		if *req.MoodScore < 1 || *req.MoodScore > 10 {
			return nil, errors.New("mood_score must be 1-10")
		}
	}
	if len(req.EmotionsText) > maxEmotionsTextLen {
		return nil, errors.New("emotions text too long")
	}
	if len(req.Notes) > maxNotesLen {
		return nil, errors.New("notes too long")
	}
	return &req, nil
}
