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
	Goals     *store.GoalStore
	CheckIns  *store.GoalCheckInStore
	Users     *store.UserStore
	ChatLLM   *llm.OpenRouter // shaper streams via the chat-tier client
	ChatModel string
	Logger    *slog.Logger
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

// ---------- /goals/draft (SMART shaper) ----------

type draftMessage struct {
	Role    string `json:"role"`    // "user" | "assistant"
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
