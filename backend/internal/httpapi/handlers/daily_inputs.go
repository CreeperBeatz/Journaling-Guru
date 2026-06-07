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

// DailyInputHandler hosts /api/daily/inputs/*. The check-in surface for
// the Energy Audit pivot's five-prompt template: 1..3 mood plus drainer
// / charger / gratitude / reflection text. Drainer/charger tags are
// attached in the same write — the handler links daily_entry_tags rows
// after the daily_inputs upsert.
type DailyInputHandler struct {
	Inputs         *store.DailyInputStore
	Users          *store.UserStore
	Tags           *store.TagStore
	DailyEntryTags *store.DailyEntryTagStore
	Logger          *slog.Logger
	Scheduler       SummaryScheduler // shares the interface with EntryHandler — same lazy-seed contract
	MemoryScheduler SummaryScheduler // same contract; arms the day's memory pass
}

const (
	maxReflectionTextLen = 4_000
	maxAuditTextLen      = 1_000 // drained / charged / gratitude
	maxTagsPerRolePerDay = 10
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

// dailyInputResponse is the GET payload. `tags` is the union of drainer
// + charger links for the day; FE splits by role for rendering.
type dailyInputResponse struct {
	LocalDate string                  `json:"local_date"`
	Input     *domain.DailyInput      `json:"input"`
	Tags      []store.TagDayLink      `json:"tags"`
}

// Get handles GET /api/daily/inputs?date=YYYY-MM-DD (or omitted = today).
// Returns 200 with the row + linked tags, or 200 with `{input: null,
// tags: [], local_date: ...}` when nothing has been logged yet.
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
	tags := []store.TagDayLink{}
	if h.DailyEntryTags != nil {
		tags, err = h.DailyEntryTags.ListByDate(r.Context(), sess.UserID, d)
		if err != nil {
			h.Logger.Error("get day tags", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "get failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, dailyInputResponse{
		LocalDate: timezone.FormatDate(d),
		Input:     row,
		Tags:      tags,
	})
}

type upsertDailyInputRequest struct {
	Mood            *int     `json:"mood"`
	DrainedText     string   `json:"drained_text"`
	ChargedText     string   `json:"charged_text"`
	GratitudeText   string   `json:"gratitude_text"`
	ReflectionText  string   `json:"reflection_text"`
	DrainedTagIDs   []string `json:"drained_tag_ids"`
	ChargedTagIDs   []string `json:"charged_tag_ids"`
}

// Upsert handles PUT /api/daily/inputs — write today's check-in.
// Empty body deletes the row, matching the journal_entries
// empty-deletes convention. Tag links are rewritten in lockstep —
// passing an empty array clears that role's tags for the day.
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
	h.write(r.Context(), w, sess.UserID, today, req, true, false)
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
	today, err := h.resolveDate(r, sess.UserID, "today")
	if err != nil {
		h.Logger.Error("resolve today", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}
	backfilled := d.Before(today)
	h.write(r.Context(), w, sess.UserID, d, req, false, backfilled)
}

func (h *DailyInputHandler) write(
	ctx context.Context, w http.ResponseWriter,
	userID string, localDate time.Time,
	req *upsertDailyInputRequest, lazySeed, backfilled bool,
) {
	row, mutated, err := h.Inputs.Upsert(ctx, userID, localDate, store.DailyInputUpsert{
		Mood:           req.Mood,
		DrainedText:    req.DrainedText,
		ChargedText:    req.ChargedText,
		GratitudeText:  req.GratitudeText,
		ReflectionText: req.ReflectionText,
		Backfilled:     backfilled,
	})
	if err != nil {
		h.Logger.Error("upsert daily input", "err", err, "user_id", userID)
		writeJSONError(w, http.StatusInternalServerError, "save failed")
		return
	}
	// Tag links are owned by the handler, not the store — we don't want
	// daily_inputs to know about tags. ReplaceForDay is idempotent.
	// On all-empty (row==nil), tag arrays are also wiped.
	if h.DailyEntryTags != nil {
		drained := req.DrainedTagIDs
		charged := req.ChargedTagIDs
		if row == nil {
			drained, charged = nil, nil
		}
		if err := h.DailyEntryTags.ReplaceForDay(ctx, userID, localDate, domain.TagRoleDrainer, drained); err != nil {
			h.Logger.Error("link drainer tags", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "link tags failed")
			return
		}
		if err := h.DailyEntryTags.ReplaceForDay(ctx, userID, localDate, domain.TagRoleCharger, charged); err != nil {
			h.Logger.Error("link charger tags", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "link tags failed")
			return
		}
	}
	if lazySeed && row != nil && h.Scheduler != nil {
		if err := h.Scheduler.LazySeed(ctx, userID, time.Now()); err != nil {
			h.Logger.Warn("lazy seed (daily input)", "err", err, "user_id", userID)
		}
	}
	if lazySeed && row != nil && h.MemoryScheduler != nil {
		if err := h.MemoryScheduler.LazySeed(ctx, userID, time.Now()); err != nil {
			h.Logger.Warn("lazy seed memory (daily input)", "err", err, "user_id", userID)
		}
	}
	if row == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"deleted":    mutated,
			"local_date": timezone.FormatDate(localDate),
		})
		return
	}
	tags := []store.TagDayLink{}
	if h.DailyEntryTags != nil {
		tags, _ = h.DailyEntryTags.ListByDate(ctx, userID, localDate)
	}
	writeJSON(w, http.StatusOK, dailyInputResponse{
		LocalDate: timezone.FormatDate(localDate),
		Input:     row,
		Tags:      tags,
	})
}

func decodeUpsert(r *http.Request) (*upsertDailyInputRequest, error) {
	var req upsertDailyInputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, errors.New("invalid json")
	}
	if req.Mood != nil {
		if *req.Mood < 1 || *req.Mood > 3 {
			return nil, errors.New("mood must be 1-3")
		}
	}
	if len(req.DrainedText) > maxAuditTextLen {
		return nil, errors.New("drained text too long")
	}
	if len(req.ChargedText) > maxAuditTextLen {
		return nil, errors.New("charged text too long")
	}
	if len(req.GratitudeText) > maxAuditTextLen {
		return nil, errors.New("gratitude text too long")
	}
	if len(req.ReflectionText) > maxReflectionTextLen {
		return nil, errors.New("reflection text too long")
	}
	if len(req.DrainedTagIDs) > maxTagsPerRolePerDay {
		return nil, errors.New("too many drainer tags")
	}
	if len(req.ChargedTagIDs) > maxTagsPerRolePerDay {
		return nil, errors.New("too many charger tags")
	}
	return &req, nil
}
