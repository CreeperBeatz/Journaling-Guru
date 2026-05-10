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
	Summaries          *store.SummaryStore
	Jobs               *store.SummaryJobStore
	Users              *store.UserStore
	DailyInputs        *store.DailyInputStore
	DailyEntryTags     *store.DailyEntryTagStore
	Goals              *store.GoalStore
	CheckIns           *store.GoalCheckInStore
	WeeklyReflections  *store.WeeklyReflectionStore
	Logger             *slog.Logger
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

// JobStatus handles GET /api/summaries/jobs/status?period_type=...&period_start=...
// Returns the summary_jobs row for the period so the FE can render a
// "Regenerating…" banner and confirm the worker actually picked the
// job up. 404 when no job has ever been scheduled for the period.
//
// Read-only — paired with /regenerate; together they let SummaryDetail
// observe the full lifecycle of a regen request.
func (h *SummaryHandler) JobStatus(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	periodType := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("period_type")))
	if !domain.IsValidPeriod(periodType) {
		writeJSONError(w, http.StatusBadRequest, "invalid period_type")
		return
	}
	periodStartStr := strings.TrimSpace(r.URL.Query().Get("period_start"))
	periodStart, err := time.Parse("2006-01-02", periodStartStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid period_start")
		return
	}
	user, err := h.Users.GetByID(r.Context(), sess.UserID)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusUnauthorized, "user not found")
		return
	}
	// Canonicalise period_start the same way Regenerate does, so
	// callers can pass a stored period_start without worrying about
	// day_start drift.
	period, err := timezone.PeriodFromLocalStart(
		periodStart, user.Timezone, user.DayStartMinutes,
		domain.SummaryPeriod(periodType),
	)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := h.Jobs.LatestForPeriod(r.Context(), sess.UserID, periodType, period.Start)
	if err != nil {
		if errors.Is(err, store.ErrSummaryJobNotFound) {
			writeJSONError(w, http.StatusNotFound, "no job")
			return
		}
		h.Logger.Error("job status", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "status failed")
		return
	}
	writeJSON(w, http.StatusOK, job)
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
	// Emotions are retired under the Energy Audit pivot — return an empty
	// array so the existing FE chart code degrades gracefully. Zone
	// endpoints below are the canonical surface; this remains for
	// backwards-compat with components that haven't migrated yet.
	writeJSON(w, http.StatusOK, map[string]any{
		"window_days": days,
		"mood":        mood,
		"emotions":    []any{},
	})
}

// ---------- Zone endpoints (Energy Audit summary page) ----------

// Spec calls out a 14-day baseline before patterns are meaningful.
const baselineDays = 14

// Zone1Response — at-a-glance: mood sparkline, 7d-vs-prior delta,
// headline insight (one sentence), active goal status (first goal +
// kept/total tally on its date range so far).
type Zone1Response struct {
	WindowDays      int                          `json:"window_days"`
	BaselineDaysReq int                          `json:"baseline_days_required"`
	HasBaseline     bool                         `json:"has_baseline"`
	Mood            []store.DailyMoodPoint       `json:"mood"`
	MoodAvg7d       *float64                     `json:"mood_avg_7d"`
	MoodAvgPrior7d  *float64                     `json:"mood_avg_prior_7d"`
	Headline        *string                      `json:"headline"`
	HeadlineFallback string                      `json:"headline_fallback"`
	ActiveGoals     []Zone1GoalStatus            `json:"active_goals"`
}

type Zone1GoalStatus struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	StartDate       string `json:"start_date"`
	EndDate         string `json:"end_date"`
	DayIndex        int    `json:"day_index"`         // 1-based; today is day N of M
	TotalDays       int    `json:"total_days"`
	KeptCount       int    `json:"kept_count"`
	AnsweredCount   int    `json:"answered_count"`
}

// Zone1 handles GET /api/summary/zone1.
func (h *SummaryHandler) Zone1(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	user, err := h.Users.GetByID(r.Context(), sess.UserID)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	today, err := timezone.LocalDate(time.Now(), user.Timezone, user.DayStartMinutes)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}

	// 30-day mood sparkline.
	mood, err := h.DailyInputs.MoodSeries(r.Context(), sess.UserID, 30)
	if err != nil {
		h.Logger.Error("zone1 mood", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}

	// Mood averages — current 7d vs prior 7d. Reuse AggregateForRange so
	// the math is centralized.
	avg7d := h.aggregateMood(r.Context(), sess.UserID, today.AddDate(0, 0, -6), today)
	avgPrior := h.aggregateMood(r.Context(), sess.UserID, today.AddDate(0, 0, -13), today.AddDate(0, 0, -7))

	// Baseline gate: how many distinct days has the user logged ANY input?
	hasBaseline, _ := h.DailyInputs.HasContentInRange(r.Context(), sess.UserID,
		today.AddDate(0, 0, -baselineDays+1), today)
	// HasContentInRange just checks existence; we want a "≥ 14 distinct days"
	// signal. Approximate via the window-30 aggregate's entry_count.
	agg, _ := h.DailyInputs.AggregateForRange(r.Context(), sess.UserID,
		today.AddDate(0, 0, -baselineDays+1), today)
	hasBaseline = agg != nil && agg.EntryCount >= baselineDays
	_ = hasBaseline // computed for completeness; the only consumer is the response

	// Headline insight — Phase 6 surfaces the latest weekly summary's
	// body as a single sentence. The worker now generates a one-liner
	// (see internal/llm/summaries/weekly_headline.go); for the pre-
	// baseline state we fall back to a queried "top drainer + avg mood"
	// sentence the FE renders without any LLM dependency.
	var headline *string
	weekStart := today.AddDate(0, 0, -6)
	weeklies, _ := h.Summaries.ListInRange(r.Context(), sess.UserID, string(domain.PeriodWeek),
		weekStart.AddDate(0, 0, -7), today)
	if len(weeklies) > 0 {
		// Pick the most recent.
		body := strings.TrimSpace(weeklies[len(weeklies)-1].Body)
		if body != "" {
			headline = &body
		}
	}

	fallback := h.headlineFallback(r.Context(), sess.UserID, weekStart, today, avg7d)

	// Active goals — show all currently-active for Zone 1 status; FE caps
	// rendering to the first one or two.
	rawGoals, err := h.Goals.ListActive(r.Context(), sess.UserID, today)
	if err != nil {
		h.Logger.Error("zone1 active goals", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	goalStatus := make([]Zone1GoalStatus, 0, len(rawGoals))
	for _, g := range rawGoals {
		startDate, _ := time.Parse("2006-01-02", g.StartDate)
		endDate, _ := time.Parse("2006-01-02", g.EndDate)
		totalDays := int(endDate.Sub(startDate).Hours()/24) + 1
		dayIndex := int(today.Sub(startDate).Hours()/24) + 1
		if dayIndex < 1 {
			dayIndex = 1
		}
		if dayIndex > totalDays {
			dayIndex = totalDays
		}
		kept, total, _ := h.CheckIns.CountKept(r.Context(), g.ID, startDate, today)
		goalStatus = append(goalStatus, Zone1GoalStatus{
			ID:            g.ID,
			Title:         g.Title,
			StartDate:     g.StartDate,
			EndDate:       g.EndDate,
			DayIndex:      dayIndex,
			TotalDays:     totalDays,
			KeptCount:     kept,
			AnsweredCount: total,
		})
	}

	writeJSON(w, http.StatusOK, Zone1Response{
		WindowDays:       30,
		BaselineDaysReq:  baselineDays,
		HasBaseline:      hasBaseline,
		Mood:             mood,
		MoodAvg7d:        avg7d,
		MoodAvgPrior7d:   avgPrior,
		Headline:         headline,
		HeadlineFallback: fallback,
		ActiveGoals:      goalStatus,
	})
}

func (h *SummaryHandler) aggregateMood(ctx context.Context, userID string, since, until time.Time) *float64 {
	if h.DailyInputs == nil {
		return nil
	}
	agg, err := h.DailyInputs.AggregateForRange(ctx, userID, since, until)
	if err != nil || agg == nil {
		return nil
	}
	return agg.MoodScore
}

// headlineFallback returns a non-LLM "top drainer · avg mood" sentence
// for Zone 1 when no weekly summary exists yet (pre-baseline). Format:
// "Top drainer this week: <label> (N days, avg mood X.X)" — falls back
// to "Still building your baseline." when there's not enough data.
func (h *SummaryHandler) headlineFallback(
	ctx context.Context, userID string, weekStart, today time.Time, mood7d *float64,
) string {
	if h.DailyEntryTags == nil {
		return "Still building your baseline."
	}
	drainers, err := h.DailyEntryTags.TopByValence(ctx, userID, domain.TagRoleDrainer, 7, 1)
	if err != nil || len(drainers) == 0 {
		_ = weekStart
		_ = today
		return "Still building your baseline."
	}
	d := drainers[0]
	moodStr := "—"
	if mood7d != nil {
		moodStr = strings.TrimSpace(strings.TrimRight(strings.TrimRight(
			fmtFloat(*mood7d, 1), "0"), "."))
	}
	return "Top drainer this week: " + d.Label +
		" (" + itoa(d.Appearances) + " day" + plural(d.Appearances) +
		", avg mood " + moodStr + ")"
}

// ---------- Zone 2: drainer + charger tables ----------

// Zone2Response — last 30 days, top drainers + chargers with avg-mood
// alongside. Low-confidence flagging is a frontend concern (renders a
// faint badge for tags with appearances < 7).
type Zone2Response struct {
	WindowDays int                     `json:"window_days"`
	Drainers   []store.TagAggregate    `json:"drainers"`
	Chargers   []store.TagAggregate    `json:"chargers"`
}

func (h *SummaryHandler) Zone2(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	days := 30
	if s := r.URL.Query().Get("days"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxStatsWindowDays {
			writeJSONError(w, http.StatusBadRequest, "invalid days")
			return
		}
		days = n
	}
	drainers, err := h.DailyEntryTags.TopByValence(r.Context(), sess.UserID, domain.TagRoleDrainer, days, 12)
	if err != nil {
		h.Logger.Error("zone2 drainers", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	chargers, err := h.DailyEntryTags.TopByValence(r.Context(), sess.UserID, domain.TagRoleCharger, days, 12)
	if err != nil {
		h.Logger.Error("zone2 chargers", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, Zone2Response{
		WindowDays: days,
		Drainers:   drainers,
		Chargers:   chargers,
	})
}

// ---------- Zone 3: goals ledger ----------

type Zone3Response struct {
	Goals []domain.Goal `json:"goals"`
}

func (h *SummaryHandler) Zone3(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	all, err := h.Goals.ListAll(r.Context(), sess.UserID)
	if err != nil {
		h.Logger.Error("zone3 goals", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, Zone3Response{Goals: all})
}

// ---------- Weekly reflection (Phase 7) ----------

// ReflectionResponse — the pattern view payload for the weekly
// reflection flow. Drives the in-place /today swap when today is the
// user's reflection_weekday, plus the wizard state (started/step/done)
// and the persisted surprise + per-mid-flight-goal notes.
type ReflectionResponse struct {
	WeekStart       string                  `json:"week_start"`
	WeekEnd         string                  `json:"week_end"`
	PriorWeekStart  string                  `json:"prior_week_start"`
	PriorWeekEnd    string                  `json:"prior_week_end"`
	MoodAvg         *float64                `json:"mood_avg"`
	MoodAvgPrior    *float64                `json:"mood_avg_prior"`
	EntryCount      int                     `json:"entry_count"`
	Drainers        []ReflectionTagRow      `json:"drainers"`
	Chargers        []ReflectionTagRow      `json:"chargers"`
	GratitudeItems  []ReflectionGratitude   `json:"gratitude_items"`
	ActiveGoals     []Zone1GoalStatus       `json:"active_goals"`

	// Wizard state — only populated for the current week. Past weeks
	// (history view) leave Started=false / Step=0 / CompletedAt=nil.
	Started      bool              `json:"started"`
	Step         int               `json:"step"`
	SurpriseText string            `json:"surprise_text"`
	GoalNotes    map[string]string `json:"goal_notes"`
	NewGoalIDs   []string          `json:"new_goal_ids"`
	CompletedAt  *time.Time        `json:"completed_at"`
}

// ReflectionTagRow extends TagAggregate with a delta against the prior
// week so the FE can show ▲/▼ next to each label.
type ReflectionTagRow struct {
	TagID       string   `json:"tag_id"`
	Label       string   `json:"label"`
	Appearances int      `json:"appearances"`
	AvgMood     *float64 `json:"avg_mood"`
	DeltaVsPrior int     `json:"delta_vs_prior"` // appearances change vs the prior week
}

type ReflectionGratitude struct {
	LocalDate string `json:"local_date"`
	Text      string `json:"text"`
}

// WeeklyReflection handles GET /api/reflection/this-week. Read-only;
// surface for the wizard. Surprise text + step + goal_notes come from
// weekly_reflections; the rest is derived from daily_inputs + tags.
func (h *SummaryHandler) WeeklyReflection(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	user, err := h.Users.GetByID(r.Context(), sess.UserID)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	today, err := timezone.LocalDate(time.Now(), user.Timezone, user.DayStartMinutes)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}
	weekStart := today.AddDate(0, 0, -6)
	resp, err := h.buildReflection(r.Context(), sess.UserID, weekStart, today, true)
	if err != nil {
		h.Logger.Error("build reflection (this week)", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ReflectionByWeek handles GET /api/reflection/by-week/{week_start}.
// Read-only past-week view used by History; loads the frozen
// weekly_reflections row alongside the recomputed pattern view.
func (h *SummaryHandler) ReflectionByWeek(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	weekStartStr := strings.TrimSpace(chi.URLParam(r, "week_start"))
	weekStart, err := time.Parse("2006-01-02", weekStartStr)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid week_start")
		return
	}
	weekEnd := weekStart.AddDate(0, 0, 6)
	resp, err := h.buildReflection(r.Context(), sess.UserID, weekStart, weekEnd, true)
	if err != nil {
		h.Logger.Error("build reflection (by week)", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// buildReflection computes the pattern view for [weekStart, weekEnd]
// (inclusive). When `includeWizardState` is true, also loads the
// weekly_reflections row for that week_start so the FE knows whether
// the wizard has been started / completed and at which step.
func (h *SummaryHandler) buildReflection(
	ctx context.Context, userID string,
	weekStart, weekEnd time.Time, includeWizardState bool,
) (*ReflectionResponse, error) {
	priorEnd := weekStart.AddDate(0, 0, -1)
	priorStart := priorEnd.AddDate(0, 0, -6)
	weekDays := int(weekEnd.Sub(weekStart).Hours()/24) + 1
	if weekDays < 1 {
		weekDays = 1
	}

	// We previously hard-coded a 7-day TopByValence call. For an
	// arbitrary [weekStart, weekEnd] window we instead anchor to the
	// past N days from weekEnd and rely on the FE seeing identical
	// numbers when called for "this week" (today == weekEnd).
	thisDrainers, _ := h.DailyEntryTags.TopByValence(ctx, userID, domain.TagRoleDrainer, weekDays, 8)
	thisChargers, _ := h.DailyEntryTags.TopByValence(ctx, userID, domain.TagRoleCharger, weekDays, 8)

	// Prior-week tag deltas — only meaningful for "this week" (where
	// weekEnd == today). For arbitrary historical weeks we still
	// compute, but the deltas may be skewed because TopByValence
	// counts back from today. The History "Weekly" tab tolerates
	// this; nothing critical depends on the delta there.
	priorDrainersAll, _ := h.DailyEntryTags.TopByValence(ctx, userID, domain.TagRoleDrainer, weekDays*2, 0)
	priorChargersAll, _ := h.DailyEntryTags.TopByValence(ctx, userID, domain.TagRoleCharger, weekDays*2, 0)
	priorDrainerCount := byTagID(subtractCurrentFromCombined(priorDrainersAll, thisDrainers))
	priorChargerCount := byTagID(subtractCurrentFromCombined(priorChargersAll, thisChargers))

	drainers := mergeWithDelta(thisDrainers, priorDrainerCount)
	chargers := mergeWithDelta(thisChargers, priorChargerCount)

	// Mood averages.
	thisAgg, _ := h.DailyInputs.AggregateForRange(ctx, userID, weekStart, weekEnd)
	priorAgg, _ := h.DailyInputs.AggregateForRange(ctx, userID, priorStart, priorEnd)

	gratitudes := []ReflectionGratitude{}
	for d := weekStart; !d.After(weekEnd); d = d.AddDate(0, 0, 1) {
		row, err := h.DailyInputs.GetByDate(ctx, userID, d)
		if err != nil || row == nil {
			continue
		}
		if g := strings.TrimSpace(row.GratitudeText); g != "" {
			gratitudes = append(gratitudes, ReflectionGratitude{
				LocalDate: row.LocalDate,
				Text:      g,
			})
		}
	}

	// Active goals as of weekEnd. For "this week" this matches the
	// previous behaviour (today as the asOf). For history, we list
	// goals that were active at the time of that week so the snapshot
	// is meaningful — `ListActive` already filters by end_date >=
	// asOf, but goals created after weekEnd would also surface.
	// Filter those out by checking start_date <= weekEnd.
	rawGoals, err := h.Goals.ListActive(ctx, userID, weekEnd)
	if err != nil {
		return nil, err
	}
	goalStatus := make([]Zone1GoalStatus, 0, len(rawGoals))
	for _, g := range rawGoals {
		startDate, _ := time.Parse("2006-01-02", g.StartDate)
		if startDate.After(weekEnd) {
			continue
		}
		endDate, _ := time.Parse("2006-01-02", g.EndDate)
		totalDays := int(endDate.Sub(startDate).Hours()/24) + 1
		dayIndex := int(weekEnd.Sub(startDate).Hours()/24) + 1
		if dayIndex < 1 {
			dayIndex = 1
		}
		if dayIndex > totalDays {
			dayIndex = totalDays
		}
		kept, total, _ := h.CheckIns.CountKept(ctx, g.ID, weekStart, weekEnd)
		goalStatus = append(goalStatus, Zone1GoalStatus{
			ID:            g.ID,
			Title:         g.Title,
			StartDate:     g.StartDate,
			EndDate:       g.EndDate,
			DayIndex:      dayIndex,
			TotalDays:     totalDays,
			KeptCount:     kept,
			AnsweredCount: total,
		})
	}

	resp := &ReflectionResponse{
		WeekStart:      timezone.FormatDate(weekStart),
		WeekEnd:        timezone.FormatDate(weekEnd),
		PriorWeekStart: timezone.FormatDate(priorStart),
		PriorWeekEnd:   timezone.FormatDate(priorEnd),
		Drainers:       drainers,
		Chargers:       chargers,
		GratitudeItems: gratitudes,
		ActiveGoals:    goalStatus,
		GoalNotes:      map[string]string{},
		NewGoalIDs:     []string{},
	}
	if thisAgg != nil {
		resp.MoodAvg = thisAgg.MoodScore
		resp.EntryCount = thisAgg.EntryCount
	}
	if priorAgg != nil {
		resp.MoodAvgPrior = priorAgg.MoodScore
	}

	if includeWizardState && h.WeeklyReflections != nil {
		wr, _ := h.WeeklyReflections.GetByWeekStart(ctx, userID, weekStart)
		if wr != nil {
			resp.Started = true
			resp.Step = wr.Step
			resp.SurpriseText = wr.SurpriseText
			resp.GoalNotes = wr.GoalNotes
			resp.NewGoalIDs = wr.NewGoalIDs
			resp.CompletedAt = wr.CompletedAt
		}
	}
	return resp, nil
}

// ----- Wizard mutating endpoints -----

// StartReflection handles POST /api/reflection/this-week/start.
// Idempotently creates the weekly_reflections row for the current week
// and returns the full ReflectionResponse so the FE can render Card 1.
func (h *SummaryHandler) StartReflection(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	weekStart, weekEnd, ok := h.resolveCurrentWeek(w, r, sess.UserID)
	if !ok {
		return
	}
	if _, err := h.WeeklyReflections.Start(r.Context(), sess.UserID, weekStart, weekEnd); err != nil {
		h.Logger.Error("start reflection", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "start failed")
		return
	}
	resp, err := h.buildReflection(r.Context(), sess.UserID, weekStart, weekEnd, true)
	if err != nil {
		h.Logger.Error("build reflection after start", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

type patchReflectionRequest struct {
	SurpriseText *string `json:"surprise_text"`
	Step         *int    `json:"step"`
	// Optional: merge a single goal note. Empty string deletes the key.
	GoalID   *string `json:"goal_id"`
	GoalNote *string `json:"goal_note"`
	// Optional: append a goal_id to new_goal_ids — used by Card 3 after
	// a commit_goal save so the Done page can split active vs new.
	NewGoalID *string `json:"new_goal_id"`
}

const maxSurpriseTextLen = 4000
const maxGoalNoteLen = 4000

// PatchReflection handles PATCH /api/reflection/this-week. Partial
// update — any of {surprise_text, step, goal_id+goal_note} can be
// supplied. Goal note is merged into goal_notes by goal_id; empty
// text removes the key. Returns the updated ReflectionResponse.
func (h *SummaryHandler) PatchReflection(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req patchReflectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.SurpriseText != nil && len(*req.SurpriseText) > maxSurpriseTextLen {
		writeJSONError(w, http.StatusBadRequest, "surprise_text too long")
		return
	}
	if req.Step != nil && (*req.Step < 1 || *req.Step > 3) {
		writeJSONError(w, http.StatusBadRequest, "step must be 1..3")
		return
	}
	if req.GoalNote != nil && len(*req.GoalNote) > maxGoalNoteLen {
		writeJSONError(w, http.StatusBadRequest, "goal_note too long")
		return
	}
	if (req.GoalID == nil) != (req.GoalNote == nil) {
		writeJSONError(w, http.StatusBadRequest, "goal_id and goal_note must be provided together")
		return
	}
	weekStart, weekEnd, ok := h.resolveCurrentWeek(w, r, sess.UserID)
	if !ok {
		return
	}
	// Lazy-create the row in case the FE patches before /start fires.
	if _, err := h.WeeklyReflections.Start(r.Context(), sess.UserID, weekStart, weekEnd); err != nil {
		h.Logger.Error("ensure reflection row", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "patch failed")
		return
	}
	if req.SurpriseText != nil || req.Step != nil {
		if _, err := h.WeeklyReflections.Patch(
			r.Context(), sess.UserID, weekStart,
			store.WeeklyReflectionPatch{
				SurpriseText: req.SurpriseText,
				Step:         req.Step,
			},
		); err != nil {
			h.Logger.Error("patch reflection", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "patch failed")
			return
		}
	}
	if req.GoalID != nil {
		if _, err := h.WeeklyReflections.SetGoalNote(
			r.Context(), sess.UserID, weekStart,
			strings.TrimSpace(*req.GoalID),
			strings.TrimSpace(*req.GoalNote),
		); err != nil {
			h.Logger.Error("set goal note", "err", err)
			writeJSONError(w, http.StatusInternalServerError, "patch failed")
			return
		}
	}
	if req.NewGoalID != nil {
		gid := strings.TrimSpace(*req.NewGoalID)
		if gid != "" {
			if _, err := h.WeeklyReflections.AddNewGoalID(
				r.Context(), sess.UserID, weekStart, gid,
			); err != nil {
				h.Logger.Error("add new goal id", "err", err)
				writeJSONError(w, http.StatusInternalServerError, "patch failed")
				return
			}
		}
	}
	resp, err := h.buildReflection(r.Context(), sess.UserID, weekStart, weekEnd, true)
	if err != nil {
		h.Logger.Error("build reflection after patch", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// CompleteReflection handles POST /api/reflection/this-week/complete.
// Sets completed_at = now() if not already set; idempotent. Returns
// the final ReflectionResponse so the FE can swap into Done view.
func (h *SummaryHandler) CompleteReflection(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	weekStart, weekEnd, ok := h.resolveCurrentWeek(w, r, sess.UserID)
	if !ok {
		return
	}
	// Lazy-create + mark complete.
	if _, err := h.WeeklyReflections.Start(r.Context(), sess.UserID, weekStart, weekEnd); err != nil {
		h.Logger.Error("ensure reflection row (complete)", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "complete failed")
		return
	}
	if _, err := h.WeeklyReflections.MarkCompleted(r.Context(), sess.UserID, weekStart); err != nil {
		h.Logger.Error("mark complete", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "complete failed")
		return
	}
	resp, err := h.buildReflection(r.Context(), sess.UserID, weekStart, weekEnd, true)
	if err != nil {
		h.Logger.Error("build reflection after complete", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "load failed")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveCurrentWeek loads the user, computes today + the
// (weekStart, weekEnd) covering "the past 7 days ending today". Writes
// an HTTP error and returns ok=false on failure.
func (h *SummaryHandler) resolveCurrentWeek(
	w http.ResponseWriter, r *http.Request, userID string,
) (time.Time, time.Time, bool) {
	user, err := h.Users.GetByID(r.Context(), userID)
	if err != nil || user == nil {
		writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
		return time.Time{}, time.Time{}, false
	}
	today, err := timezone.LocalDate(time.Now(), user.Timezone, user.DayStartMinutes)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return time.Time{}, time.Time{}, false
	}
	return today.AddDate(0, 0, -6), today, true
}

// subtractCurrentFromCombined returns just the prior-week portion of a
// 14-day aggregate by subtracting current-week appearance counts from
// the combined totals. Tags only present in the current week become 0.
func subtractCurrentFromCombined(combined, current []store.TagAggregate) []store.TagAggregate {
	currentByID := byTagID(current)
	out := make([]store.TagAggregate, 0, len(combined))
	for _, c := range combined {
		priorCount := c.Appearances - currentByID[c.TagID]
		if priorCount <= 0 {
			continue
		}
		c.Appearances = priorCount
		out = append(out, c)
	}
	return out
}

func byTagID(rows []store.TagAggregate) map[string]int {
	m := make(map[string]int, len(rows))
	for _, r := range rows {
		m[r.TagID] = r.Appearances
	}
	return m
}

func mergeWithDelta(current []store.TagAggregate, prior map[string]int) []ReflectionTagRow {
	out := make([]ReflectionTagRow, 0, len(current))
	for _, c := range current {
		prev := prior[c.TagID]
		out = append(out, ReflectionTagRow{
			TagID:        c.TagID,
			Label:        c.Label,
			Appearances:  c.Appearances,
			AvgMood:      c.AvgMood,
			DeltaVsPrior: c.Appearances - prev,
		})
	}
	return out
}

// Tiny helpers (kept local to avoid pulling fmt/strconv dependencies
// into the handler's hot paths).
func fmtFloat(f float64, digits int) string {
	// strconv is already imported at the top.
	return strconv.FormatFloat(f, 'f', digits, 64)
}
func itoa(n int) string { return strconv.Itoa(n) }
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
