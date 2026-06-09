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
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/goals"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// GoalHandler hosts /api/goals/*. Goals close the loop between "spotted
// a pattern" and "tried to change something." Each active goal renders
// a yes/no daily check-in on /today.
type GoalHandler struct {
	Goals          *store.GoalStore
	CheckIns       *store.GoalCheckInStore
	Users          *store.UserStore
	DailyEntryTags *store.DailyEntryTagStore
	DailyInputs    *store.DailyInputStore
	ChatLLM        *llm.OpenRouter // shaper streams via the chat-tier client
	ClassifyLLM    *llm.OpenRouter // suggest uses the cheap classify-tier client
	ChatModel      string
	ClassifyModel  string
	Logger         *slog.Logger
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
			"goals":            goals,
			"todays_check_ins": checkins,
			"local_date":       timezone.FormatDate(today),
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
	Title           string `json:"title"`
	CheckInQuestion string `json:"check_in_question"`
	// WhyMatters / IfFollowed / IfNotFollowed are the user's own words on
	// why they chose this goal. Populated by the weekly reflection
	// companion via the propose_goal tool; manual creators pass empty
	// strings and the server stores ''.
	WhyMatters    string  `json:"why_matters"`
	IfFollowed    string  `json:"if_followed"`
	IfNotFollowed string  `json:"if_not_followed"`
	EndDate       string  `json:"end_date"`       // YYYY-MM-DD; used only when duration_weeks omitted
	DurationWeeks *int    `json:"duration_weeks"` // 1..52; preferred — server snaps end to next reflection_weekday
	StartDate     *string `json:"start_date"`     // optional; defaults to today
}

const maxGoalMotivationLen = 600

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
	whyMatters := strings.TrimSpace(req.WhyMatters)
	ifFollowed := strings.TrimSpace(req.IfFollowed)
	ifNotFollowed := strings.TrimSpace(req.IfNotFollowed)
	if len(whyMatters) > maxGoalMotivationLen ||
		len(ifFollowed) > maxGoalMotivationLen ||
		len(ifNotFollowed) > maxGoalMotivationLen {
		writeJSONError(w, http.StatusBadRequest, "motivation fields capped at 600 chars")
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
	startDate := today
	if req.StartDate != nil && strings.TrimSpace(*req.StartDate) != "" {
		parsed, err := time.Parse("2006-01-02", *req.StartDate)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "start_date must be YYYY-MM-DD")
			return
		}
		startDate = parsed
	}

	// Goals always end on the user's reflection_weekday so every goal
	// terminates inside a weekly reflection. Prefer duration_weeks
	// (server-computed); fall back to snapping a caller-supplied
	// end_date forward to the next reflection_weekday at-or-after.
	var endDate time.Time
	weekday := user.ReflectionWeekday
	if req.DurationWeeks != nil {
		n := *req.DurationWeeks
		if n < 1 || n > 52 {
			writeJSONError(w, http.StatusBadRequest, "duration_weeks must be 1..52")
			return
		}
		endDate, err = timezone.NextReflectionWeekday(startDate, weekday, n)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	} else {
		parsed, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "end_date must be YYYY-MM-DD or supply duration_weeks")
			return
		}
		// Snap forward to the first reflection_weekday >= parsed.
		// Equivalent to NextReflectionWeekday(parsed-1, weekday, 1).
		anchor := parsed.AddDate(0, 0, -1)
		endDate, err = timezone.NextReflectionWeekday(anchor, weekday, 1)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if endDate.Before(startDate) {
		writeJSONError(w, http.StatusBadRequest, "end_date must be on or after start_date")
		return
	}
	if endDate.Sub(startDate).Hours()/24 > maxGoalDurationDays {
		writeJSONError(w, http.StatusBadRequest, "goals capped at one year")
		return
	}

	goal, err := h.Goals.Create(r.Context(), store.CreateGoalInput{
		UserID:          sess.UserID,
		Title:           title,
		CheckInQuestion: question,
		WhyMatters:      whyMatters,
		IfFollowed:      ifFollowed,
		IfNotFollowed:   ifNotFollowed,
		StartDate:       startDate,
		EndDate:         endDate,
	})
	if err != nil {
		h.Logger.Error("create goal", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, goal)
}

type updateGoalRequest struct {
	Action         string `json:"action"`  // "complete" | "abandon" | "extend"
	Outcome        string `json:"outcome"` // for complete: "kept" | "dropped" | "inconclusive"
	ConclusionText string `json:"conclusion_text"`
	ExtendWeeks    int    `json:"extend_weeks"` // for extend: 1..12 reflection_weekday hops
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
	case "extend":
		if req.ExtendWeeks < 1 || req.ExtendWeeks > 12 {
			writeJSONError(w, http.StatusBadRequest, "extend_weeks must be 1..12")
			return
		}
		// Look up the goal so we can advance its end_date by N
		// reflection_weekday hops from the current end_date (not from
		// today) — keeps the cadence aligned even if the user extends
		// after the original end_date has passed.
		existing, gerr := h.Goals.GetByID(r.Context(), sess.UserID, id)
		if gerr != nil || existing == nil {
			writeJSONError(w, http.StatusNotFound, "goal not found")
			return
		}
		user, uerr := h.Users.GetByID(r.Context(), sess.UserID)
		if uerr != nil || user == nil {
			writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
			return
		}
		curEnd, perr := time.Parse("2006-01-02", existing.EndDate)
		if perr != nil {
			writeJSONError(w, http.StatusInternalServerError, "bad goal end_date")
			return
		}
		newEnd, nerr := timezone.NextReflectionWeekday(curEnd, user.ReflectionWeekday, req.ExtendWeeks)
		if nerr != nil {
			writeJSONError(w, http.StatusBadRequest, nerr.Error())
			return
		}
		addDays := int(newEnd.Sub(curEnd).Hours() / 24)
		goal, err = h.Goals.Extend(r.Context(), sess.UserID, id, addDays)
	default:
		writeJSONError(w, http.StatusBadRequest, "action must be complete|abandon|extend")
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
	// Compare on the calendar-date string, not on time.Time instants:
	// `date` is midnight in the user's IANA zone (from timezone.LocalDate),
	// while parsing goal.StartDate/EndDate as YYYY-MM-DD yields midnight
	// UTC. For any non-UTC zone those instants disagree by the offset,
	// so an instant comparison incorrectly rejects same-day check-ins.
	checkInDate := date.Format("2006-01-02")
	if checkInDate < goal.StartDate || checkInDate > goal.EndDate {
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

// ---------- /goals/suggest (chip suggestions for the weekly wizard) ----------

// Suggest handles POST /api/goals/suggest. Returns up to 3 SMART
// goal candidates derived from the user's recent week pattern. Used
// by the weekly reflection wizard's Card 3 to pre-seed the shaper
// conversation with chip options. Cheap classify-tier model.
func (h *GoalHandler) Suggest(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if h.ClassifyLLM == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "suggest unavailable")
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
	const weekDays = 7
	drainers, _ := h.DailyEntryTags.TopByValence(r.Context(), sess.UserID, domain.TagRoleDrainer, weekDays, 5)
	chargers, _ := h.DailyEntryTags.TopByValence(r.Context(), sess.UserID, domain.TagRoleCharger, weekDays, 5)
	agg, _ := h.DailyInputs.AggregateForRange(r.Context(), sess.UserID,
		today.AddDate(0, 0, -(weekDays-1)), today)

	in := goals.SuggestionInput{
		WeekDays:    weekDays,
		TopDrainers: tagAggregatesToPatterns(drainers),
		TopChargers: tagAggregatesToPatterns(chargers),
	}
	if agg != nil {
		in.MoodAvg = agg.MoodScore
	}
	rawGoals, _ := h.Goals.ListActive(r.Context(), sess.UserID, today)
	for _, g := range rawGoals {
		in.ActiveGoals = append(in.ActiveGoals, g.Title)
	}

	out, err := goals.Suggest(r.Context(), h.ClassifyLLM, h.ClassifyModel, in)
	if err != nil {
		h.Logger.Error("goal suggest", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusBadGateway, "suggest failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out})
}

func tagAggregatesToPatterns(rows []store.TagAggregate) []goals.TagPattern {
	out := make([]goals.TagPattern, 0, len(rows))
	for _, r := range rows {
		out = append(out, goals.TagPattern{
			Label:       r.Label,
			Appearances: r.Appearances,
			AvgMood:     r.AvgMood,
		})
	}
	return out
}

// ---------- /goals/draft (SMART shaper) ----------

type draftMessage struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

type draftRequest struct {
	Messages []draftMessage `json:"messages"`
	// Opener=true on the first call: the FE has just opened the modal
	// with no conversation yet and wants the shaper's welcoming first
	// message. Server injects the opener instruction.
	Opener bool `json:"opener"`
}

const (
	maxDraftTurns        = 24   // hard cap on conversation depth
	maxDraftMessageChars = 4000 // per turn
	shaperMaxTokens      = 400  // small replies; tool calls don't need much
)

// Draft handles POST /api/goals/draft. Streams a single assistant turn
// from the SMART shaper given the prior conversation. Stateless on the
// server — the FE owns the message history and resends it each turn.
//
// SSE event vocabulary mirrors /api/chat (token / tool / done / error)
// so the FE can reuse its parser.
func (h *GoalHandler) Draft(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if h.ChatLLM == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "shaper not configured")
		return
	}
	var req draftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Messages) > maxDraftTurns {
		writeJSONError(w, http.StatusBadRequest, "conversation too long")
		return
	}

	llmMsgs := make([]llm.Message, 0, len(req.Messages)+1)
	for _, m := range req.Messages {
		role := strings.TrimSpace(m.Role)
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		if len(content) > maxDraftMessageChars {
			content = content[:maxDraftMessageChars]
		}
		switch role {
		case "user", "assistant":
			llmMsgs = append(llmMsgs, llm.Message{Role: role, Content: content})
		default:
			// Drop unknown roles silently; never trust client input.
			continue
		}
	}

	if req.Opener {
		// Synthetic priming user turn that's NOT echoed back to the FE
		// — the system prompt's rules + this nudge make the model
		// produce the welcoming first message.
		llmMsgs = []llm.Message{{Role: "user", Content: goals.ShaperOpenerInstruction}}
	} else if len(llmMsgs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "empty conversation")
		return
	}

	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	writeSSEHeaders(w)

	model := h.ChatModel
	chunks, err := h.ChatLLM.CompleteStream(r.Context(), llm.StreamRequest{
		Model:           model,
		System:          goals.ShaperSystemPrompt,
		Messages:        llmMsgs,
		Tools:           goals.ShaperTools,
		MaxTokens:       shaperMaxTokens,
		Temperature:     0.5,
		SystemCacheable: true,
	})
	if err != nil {
		writeSSEFrame(w, rc, "error", map[string]any{"message": err.Error()})
		return
	}

	type heldToolCall struct {
		Name string
		Args map[string]any
	}
	var fullContent strings.Builder
	var toolCalls []heldToolCall
	var done *llm.StreamDone
	var streamErr error

	for chunk := range chunks {
		if r.Context().Err() != nil {
			for range chunks {
			}
			return
		}
		if chunk.Err != nil {
			streamErr = chunk.Err
			break
		}
		if chunk.Delta != "" {
			fullContent.WriteString(chunk.Delta)
			if !writeSSEFrame(w, rc, "token", map[string]any{"delta": chunk.Delta}) {
				return
			}
		}
		if chunk.ToolCall != nil {
			toolCalls = append(toolCalls, heldToolCall{Name: chunk.ToolCall.Name, Args: chunk.ToolCall.Args})
		}
		if chunk.Done != nil {
			done = chunk.Done
		}
	}

	if streamErr != nil {
		writeSSEFrame(w, rc, "error", map[string]any{"message": streamErr.Error()})
		return
	}

	for _, tc := range toolCalls {
		writeSSEFrame(w, rc, "tool", map[string]any{
			"name": tc.Name,
			"args": tc.Args,
		})
	}

	donePayload := map[string]any{}
	if done != nil {
		donePayload["prompt_tokens"] = done.PromptTokens
		donePayload["completion_tokens"] = done.CompletionTokens
		donePayload["finish_reason"] = done.FinishReason
		donePayload["model"] = done.Model
	}
	writeSSEFrame(w, rc, "done", donePayload)
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
