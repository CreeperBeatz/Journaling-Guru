package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/chat"
	"github.com/cosmosthrace/journai/backend/internal/llm/realtime"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// ChatHandler hosts /api/chat/*. The streaming endpoints are mounted on
// a router group that omits the global 30s chimw.Timeout so an SSE
// response can stay open through the LLM round-trip.
//
// Two LLM clients: ChatLLM (CHAT_MODEL, latency-sensitive streaming) and
// ClassifyLLM (CLASSIFY_MODEL, JSON-mode coverage classifier). Per-call
// model overrides still flow through StreamRequest/CompletionRequest, so
// session pins on chat_sessions take precedence over the env defaults.
type ChatHandler struct {
	Sessions       *store.ChatSessionStore
	Messages       *store.ChatMessageStore
	Jobs           *store.ChatExtractionJobStore
	Questions      *store.QuestionStore
	Goals          *store.GoalStore
	Users          *store.UserStore
	DailyInputs    *store.DailyInputStore
	// WeeklyReflections, Summaries, and DailyEntryTags are populated for
	// the weekly reflection chat (scope='weekly') — used to seed the
	// system prompt with the letter + patterns and to mark the wizard
	// completed at wrap-up. Nil for callers that only need daily chat
	// (legacy), but the router wires them all.
	WeeklyReflections *store.WeeklyReflectionStore
	Summaries         *store.SummaryStore
	DailyEntryTags    *store.DailyEntryTagStore
	ChatLLM        *llm.OpenRouter
	ClassifyLLM    *llm.OpenRouter
	// Realtime is the OpenAI Realtime API client used by /voice/start.
	// Nil-tolerant: when OPENAI_API_KEY is unset the field is non-nil
	// but its APIKey is empty, so MintEphemeralSecret returns
	// realtime.ErrNoAPIKey and the handler responds 503.
	Realtime       *realtime.Client
	Logger         *slog.Logger
	ChatModel      string
	ClassifyModel  string
	RealtimeModel  string
	MaxTurns       int
	HardCapMinutes int
	KeepLastN      int
	ResourcesURL   string
}

// chat-mode bounds — clipped pre-LLM so a runaway client can't pass
// 1MB user prompts.
const (
	maxChatContentLen     = 4_000
	chatAssistantMaxTokens = 240 // soft ceiling; system prompt targets 40-80
	chatOpenerMaxTokens    = 120 // greeting is shorter
)

// chatSessionEnvelope is the shared shape returned by /sessions endpoints
// — session row + transcript. The frontend reads both atomically so it
// never has to render a stale transcript against a fresh session.
type chatSessionEnvelope struct {
	Session  *domain.ChatSession   `json:"session"`
	Messages []domain.ChatMessage  `json:"messages"`
}

// ---------- helpers ----------

// resolveToday returns the local-date for "today" in the user's IANA
// timezone + day_start_minutes offset. Mirrors the entries handler.
func (h *ChatHandler) resolveToday(r *http.Request, userID string) (time.Time, *domain.User, error) {
	u, err := h.Users.GetByID(r.Context(), userID)
	if err != nil {
		return time.Time{}, nil, err
	}
	if u == nil {
		return time.Time{}, nil, errors.New("user not found")
	}
	d, err := timezone.LocalDate(time.Now(), u.Timezone, u.DayStartMinutes)
	if err != nil {
		return time.Time{}, nil, err
	}
	return d, u, nil
}

// loadEnvelope loads the session + its transcript scoped to user.
// 404 / nil-session is the "no session for today" condition.
func (h *ChatHandler) loadEnvelope(ctx context.Context, userID, sessionID string) (*chatSessionEnvelope, error) {
	session, err := h.Sessions.GetByID(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	messages, err := h.Messages.ListBySession(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	return &chatSessionEnvelope{Session: session, Messages: messages}, nil
}

// writeSSEHeaders sets up the response for chunked text/event-stream.
// X-Accel-Buffering disables nginx-style buffering at any reverse proxy
// in front of us (Caddy by default does the right thing, but we set the
// header for portability).
func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// writeSSEFrame emits one SSE frame and flushes. We use
// http.NewResponseController so the Flush call walks any wrapper
// middleware (AccessLog's statusRecorder, etc.) instead of failing the
// bare `w.(http.Flusher)` cast on the outer wrapper.
func writeSSEFrame(w http.ResponseWriter, rc *http.ResponseController, event string, data any) bool {
	buf, err := json.Marshal(data)
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, buf); err != nil {
		return false
	}
	if err := rc.Flush(); err != nil {
		return false
	}
	return true
}

// ---------- /sessions/today ----------

// Today handles GET /api/chat/sessions/today. Returns
// {session: ChatSession|null, messages: ChatMessage[]} — the FE checks
// session==null to decide whether to call POST /sessions.
func (h *ChatHandler) Today(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	d, _, err := h.resolveToday(r, sess.UserID)
	if err != nil {
		h.Logger.Error("resolve today", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}
	session, err := h.Sessions.GetByDate(r.Context(), sess.UserID, d)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: nil, Messages: []domain.ChatMessage{}})
			return
		}
		h.Logger.Error("get session by date", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	messages, err := h.Messages.ListBySession(r.Context(), session.ID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: session, Messages: messages})
}

// ---------- /sessions (create-or-resume) ----------

// CreateOrResume handles POST /api/chat/sessions. Idempotent — if a
// session for (user, today) exists it's returned untouched; otherwise
// a fresh greeting-phase row is inserted.
func (h *ChatHandler) CreateOrResume(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	d, _, err := h.resolveToday(r, sess.UserID)
	if err != nil {
		h.Logger.Error("resolve today", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "could not resolve today")
		return
	}
	session, _, err := h.Sessions.CreateOrResume(r.Context(), sess.UserID, d, h.ChatModel, h.ClassifyModel)
	if err != nil {
		h.Logger.Error("create chat session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	messages, err := h.Messages.ListBySession(r.Context(), session.ID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: session, Messages: messages})
}

// ---------- /sessions/by-date/:date ----------

// ByDate handles GET /api/chat/sessions/by-date/:date. Used by the
// HistoryView transcript card (read-only). Returns 404 when the user
// has no session for that day.
func (h *ChatHandler) ByDate(w http.ResponseWriter, r *http.Request) {
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
	session, err := h.Sessions.GetByDate(r.Context(), sess.UserID, d)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		h.Logger.Error("get session by date", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	messages, err := h.Messages.ListBySession(r.Context(), session.ID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: session, Messages: messages})
}

// ---------- /reflection/this-week/chat (weekly create-or-resume) ----------

// resolveCurrentWeek returns weekStart (today - 6 days) and weekEnd
// (today) for the requesting user, mirroring the SummaryHandler helper.
// Single source of truth for "this week" within the chat handler so the
// reflection chat keys align with the wizard's view.
func (h *ChatHandler) resolveCurrentWeek(r *http.Request, userID string) (time.Time, time.Time, *domain.User, error) {
	today, user, err := h.resolveToday(r, userID)
	if err != nil {
		return time.Time{}, time.Time{}, nil, err
	}
	weekStart := today.AddDate(0, 0, -6)
	return weekStart, today, user, nil
}

// CreateOrResumeWeekly handles POST /api/reflection/this-week/chat.
// Idempotent — returns the existing weekly chat session if one exists
// for (user, week_start), otherwise inserts a fresh greeting-phase row
// with scope='weekly'. Also writes the chat_session_id FK onto the
// weekly_reflections row so the wizard / DonePage can resolve the
// session in one query.
func (h *ChatHandler) CreateOrResumeWeekly(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	weekStart, weekEnd, _, err := h.resolveCurrentWeek(r, sess.UserID)
	if err != nil {
		h.Logger.Error("resolve current week", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "could not resolve week")
		return
	}
	// Lazy-seed the reflection row so the FK target exists.
	if h.WeeklyReflections != nil {
		if _, err := h.WeeklyReflections.Start(r.Context(), sess.UserID, weekStart, weekEnd); err != nil {
			h.Logger.Warn("ensure reflection row (weekly chat)", "err", err)
		}
	}
	session, created, err := h.Sessions.CreateOrResumeWeekly(
		r.Context(), sess.UserID, weekStart, h.ChatModel, h.ClassifyModel,
	)
	if err != nil {
		h.Logger.Error("create weekly chat session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	if h.WeeklyReflections != nil {
		if _, err := h.WeeklyReflections.SetChatSession(r.Context(), sess.UserID, weekStart, session.ID); err != nil {
			h.Logger.Warn("set chat_session_id on reflection", "err", err)
		}
	}

	// Seed the letter as the first assistant turn so the user can SEE
	// the letter while the chat asks them about it. The closing question
	// lives at the bottom of the bubble so the model's first prompt is
	// already in the transcript — no separate streamed opener needed.
	if created {
		body := h.composeWeeklyLetterOpener(r.Context(), sess.UserID, weekStart)
		if body != "" {
			if _, aerr := h.Messages.Append(r.Context(), store.AppendInput{
				SessionID: session.ID,
				Role:      domain.ChatRoleAssistant,
				Content:   body,
			}); aerr != nil {
				h.Logger.Warn("seed weekly letter opener", "err", aerr, "session_id", session.ID)
			} else {
				// Advance phase from greeting → exploring; the assistant
				// has "spoken" and the FE's auto-opener will skip.
				if updated, perr := h.Sessions.AdvancePhase(r.Context(), session.ID, domain.ChatPhaseExploring); perr == nil {
					session = updated
				}
			}
		}
	}

	messages, err := h.Messages.ListBySession(r.Context(), session.ID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: session, Messages: messages})
}

// composeWeeklyLetterOpener assembles the four structured paragraphs +
// closing question into a single multi-paragraph string suitable for an
// assistant chat bubble. Falls back to the legacy `letter` blob when
// the structured fields are empty. Returns "" if no letter content
// exists at all (the FE then sees an empty transcript; the model's
// regular auto-opener fires).
func (h *ChatHandler) composeWeeklyLetterOpener(
	ctx context.Context, userID string, weekStart time.Time,
) string {
	if h.Summaries == nil {
		return ""
	}
	weekEnd := weekStart.AddDate(0, 0, 6)
	summary, _ := h.Summaries.GetByPeriod(ctx, userID, string(domain.PeriodWeek), weekStart)
	if summary == nil || !summary.Metadata.HasLetterSynthesis() {
		if latest, _ := h.Summaries.LatestByPeriodTypeUpTo(ctx, userID, string(domain.PeriodWeek), weekEnd); latest != nil && latest.Metadata.HasLetterSynthesis() {
			summary = latest
		}
	}
	if summary == nil {
		return ""
	}
	m := summary.Metadata
	var parts []string
	if m.Charged != "" {
		parts = append(parts, m.Charged)
	}
	if m.Drained != "" {
		parts = append(parts, m.Drained)
	}
	if m.Grateful != "" {
		parts = append(parts, m.Grateful)
	}
	if m.Insights != "" {
		parts = append(parts, m.Insights)
	}
	if len(parts) == 0 && m.Letter != "" {
		parts = append(parts, m.Letter)
	}
	if m.ClosingQuestion != "" {
		parts = append(parts, m.ClosingQuestion)
	}
	return strings.Join(parts, "\n\n")
}

// ThisWeekChat handles GET /api/reflection/this-week/chat. Returns the
// existing weekly session + transcript, or {session:null, messages:[]}
// when no session has been created yet (FE then calls the POST).
func (h *ChatHandler) ThisWeekChat(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	weekStart, _, _, err := h.resolveCurrentWeek(r, sess.UserID)
	if err != nil {
		h.Logger.Error("resolve current week", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "could not resolve week")
		return
	}
	session, err := h.Sessions.GetByWeek(r.Context(), sess.UserID, weekStart)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: nil, Messages: []domain.ChatMessage{}})
			return
		}
		h.Logger.Error("get weekly session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	messages, err := h.Messages.ListBySession(r.Context(), session.ID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: session, Messages: messages})
}

// ChatByWeek handles GET /api/reflection/by-week/:week_start/chat.
// Historical read-only transcript surface. 404 when the user has no
// weekly chat for that week.
func (h *ChatHandler) ChatByWeek(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	weekParam := chi.URLParam(r, "week_start")
	weekStart, err := time.Parse("2006-01-02", weekParam)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid week_start")
		return
	}
	session, err := h.Sessions.GetByWeek(r.Context(), sess.UserID, weekStart)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		h.Logger.Error("get weekly session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	messages, err := h.Messages.ListBySession(r.Context(), session.ID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, chatSessionEnvelope{Session: session, Messages: messages})
}

// ---------- /sessions/:id/system-event ----------

// systemEventRequest carries one of the whitelisted user-driven events
// the FE injects so the assistant sees the user's interaction with an
// inline UI surface on the next turn.
type systemEventRequest struct {
	Content string `json:"content"`
}

// allowedSystemEvents is the closed set of content strings the FE may
// inject via the system-event endpoint. Anything else is rejected.
var allowedSystemEvents = map[string]struct{}{
	"user_accepted_goal":          {},
	"user_declined_goal":          {},
	"user_edited_goal":            {},
	"user_accepted_extend_goal":   {},
	"user_declined_extend_goal":   {},
	"user_accepted_complete_goal": {},
	"user_declined_complete_goal": {},
}

// AppendSystemEvent handles POST /api/chat/sessions/:id/system-event.
// Appends a system_event chat_message with one of the whitelisted
// content strings. Used by the FE to surface inline-card interactions
// (Accept / Decline on propose_goal etc.) into the transcript so the
// next assistant turn can react.
func (h *ChatHandler) AppendSystemEvent(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	var req systemEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	content := strings.TrimSpace(req.Content)
	if _, ok := allowedSystemEvents[content]; !ok {
		writeJSONError(w, http.StatusBadRequest, "unknown event")
		return
	}
	if _, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID); err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	row, err := h.Messages.Append(r.Context(), store.AppendInput{
		SessionID: sessionID,
		Role:      domain.ChatRoleSystemEvent,
		Content:   content,
	})
	if err != nil {
		h.Logger.Error("append system_event", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "persist failed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message_id": row.ID, "seq": row.Seq})
}

// ---------- /sessions/:id/messages (SSE stream) ----------

type streamMessageRequest struct {
	Content string `json:"content"`
}

// StreamMessage handles POST /api/chat/sessions/:id/messages. Body is
// {content}; response is an SSE stream. The handler:
//  1. Validates session ownership and phase (must be non-finalized).
//  2. Runs the safety regex. On hit: persists user msg + system_event,
//     emits one `event: crisis` frame, returns. No LLM call.
//  3. Persists the user msg.
//  4. Greeting → exploring on first user turn.
//  5. Builds system prompt + transcript window, calls CompleteStream.
//  6. For each chunk, emits the matching SSE event and accumulates the
//     full assistant content + tool calls.
//  7. On stream done: persists assistant msg, emits any tool/phase
//     frames, emits `event: done` with usage stats.
func (h *ChatHandler) StreamMessage(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}

	var req streamMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeJSONError(w, http.StatusBadRequest, "content required")
		return
	}
	if len(content) > maxChatContentLen {
		writeJSONError(w, http.StatusBadRequest, "content too long")
		return
	}

	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	// No phase-based blocking for DAILY chat. It stays open per
	// (user, local_date) all day; finalize is just a "refresh the
	// check-in now" trigger, not a lock. If the session was previously
	// finalized, the first new user turn rolls phase back to exploring
	// (handled below).

	// Turn cap check (user + assistant only — tool / system_event don't
	// count). Hard cap is enforced server-side; the FE's hard-cap-minute
	// timer is advisory.
	turns, err := h.Messages.CountTurns(r.Context(), sessionID)
	if err != nil {
		h.Logger.Error("count turns", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if turns >= h.MaxTurns {
		writeJSONError(w, http.StatusConflict, "session over turn cap; finalize")
		return
	}

	rc := http.NewResponseController(w)

	// Crisis short-circuit. The user's message is persisted (so the UI
	// transcript remains accurate); a system_event row is appended so
	// future assistant turns see the dismissal context. No LLM call.
	if chat.IsCrisis(content) {
		if _, err := h.Messages.Append(r.Context(), store.AppendInput{
			SessionID: sessionID,
			Role:      domain.ChatRoleUser,
			Content:   content,
		}); err != nil {
			h.Logger.Error("append user (crisis)", "err", err)
		}
		eventName := "crisis_detected"
		evtName := eventName
		if _, err := h.Messages.Append(r.Context(), store.AppendInput{
			SessionID: sessionID,
			Role:      domain.ChatRoleSystemEvent,
			Content:   evtName,
		}); err != nil {
			h.Logger.Error("append system_event (crisis)", "err", err)
		}
		_ = h.Sessions.TouchActivity(r.Context(), sessionID)

		writeSSEHeaders(w)
		writeSSEFrame(w, rc, "crisis", map[string]any{
			"reason":        "self_harm_signal",
			"resources_url": h.ResourcesURL,
		})
		return
	}

	// Persist the user message.
	userMsg, err := h.Messages.Append(r.Context(), store.AppendInput{
		SessionID: sessionID,
		Role:      domain.ChatRoleUser,
		Content:   content,
	})
	if err != nil {
		h.Logger.Error("append user message", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "persist failed")
		return
	}
	_ = h.Sessions.TouchActivity(r.Context(), sessionID)

	// Reset phase to exploring on:
	//   - greeting → exploring (first user reply)
	//   - finalized → exploring (user resumes after a previous extraction)
	// wrapping_up is intentionally NOT reset on user reply: once the
	// user has signaled wrap-up (via the affordance) the model needs
	// to keep "land it" framing across the follow-up answer(s) until
	// it calls propose_wrap_up or the user explicitly cancels via
	// the kebab (POST /wrap-up/cancel).
	phaseFrameNeeded := ""
	if session.Phase != domain.ChatPhaseExploring && session.Phase != domain.ChatPhaseWrappingUp {
		if updated, err := h.Sessions.AdvancePhase(r.Context(), sessionID, domain.ChatPhaseExploring); err == nil {
			session = updated
			phaseFrameNeeded = domain.ChatPhaseExploring
		}
	}

	h.runStream(w, r, rc, session, false, phaseFrameNeeded, userMsg)
}

// ---------- /sessions/:id/opener (SSE stream, no user input) ----------

// Opener handles GET /api/chat/sessions/:id/opener. Streams the model's
// opening greeting for a fresh greeting-phase session. Idempotent: if
// any assistant message already exists, returns 409 — the FE should
// fall back to rendering the persisted opener instead.
func (h *ChatHandler) Opener(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	if session.Phase != domain.ChatPhaseGreeting {
		writeJSONError(w, http.StatusConflict, "session already opened")
		return
	}
	existing, err := h.Messages.ListBySession(r.Context(), sessionID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	for _, m := range existing {
		if m.Role == domain.ChatRoleAssistant {
			writeJSONError(w, http.StatusConflict, "opener already streamed")
			return
		}
	}

	rc := http.NewResponseController(w)

	h.runStream(w, r, rc, session, true, "", nil)
}

// ---------- /sessions/:id/finalize ----------

// Finalize handles POST /api/chat/sessions/:id/finalize. Schedules the
// extraction job and returns 202 with the job state. Idempotent —
// double-clicks within the same minute return the same job. Body is
// ignored; the worker always merges silently (LLM-merge for non-empty
// conflicts, manual fields preserved when nothing was extracted).
func (h *ChatHandler) Finalize(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}

	// Weekly finalize is a different machine: no extraction job, no
	// daily_inputs merge. Distil one continuity sentence from the
	// transcript, write it into weekly_reflections.surprise_text, mark
	// the reflection completed, and advance the chat phase to finalized.
	// The FE then redirects to /weekly (DonePage).
	if session.Scope == domain.ChatScopeWeekly {
		if updated, err := h.Sessions.AdvancePhase(r.Context(), sessionID, domain.ChatPhaseFinalized); err == nil {
			session = updated
		} else if !errors.Is(err, store.ErrChatSessionInvalidPhase) {
			h.Logger.Warn("advance weekly phase finalized", "err", err, "session_id", sessionID)
		}
		// Best-effort surprise extract. Failures don't block wrap-up.
		transcript, lerr := h.Messages.ListBySession(r.Context(), session.ID)
		if lerr != nil {
			h.Logger.Warn("weekly finalize: load transcript", "err", lerr, "session_id", session.ID)
		} else if h.ClassifyLLM != nil {
			surpriseText, eerr := chat.ExtractWeeklySurprise(r.Context(), h.ClassifyLLM, chat.ExtractWeeklySurpriseParams{
				Model:    session.ExtractionModel,
				Messages: transcript,
			})
			if eerr != nil {
				h.Logger.Warn("weekly surprise extract", "err", eerr, "session_id", session.ID)
			} else if h.WeeklyReflections != nil && session.PeriodStart != nil {
				if ws, perr := time.Parse("2006-01-02", *session.PeriodStart); perr == nil {
					if _, serr := h.WeeklyReflections.SetSurpriseText(
						r.Context(), session.UserID, ws, surpriseText,
					); serr != nil {
						h.Logger.Warn("set surprise text", "err", serr, "session_id", session.ID)
					}
				}
			}
		}
		if h.WeeklyReflections != nil {
			if _, err := h.WeeklyReflections.MarkCompletedBySession(r.Context(), session.ID); err != nil {
				h.Logger.Warn("mark reflection completed", "err", err, "session_id", session.ID)
			}
		}
		// Set extraction_status completed so the FE polling loop closes
		// out (we reuse the same envelope shape as daily finalize).
		_ = h.Sessions.SetExtractionStatus(r.Context(), session.ID, domain.ChatExtractionCompleted, nil)
		_ = h.Sessions.MarkFinalized(r.Context(), session.ID)
		writeJSON(w, http.StatusAccepted, map[string]any{
			"session_id":        session.ID,
			"extraction_status": domain.ChatExtractionCompleted,
			"phase":             domain.ChatPhaseFinalized,
			"poll_status_url":   fmt.Sprintf("/api/chat/sessions/%s/extraction/status", session.ID),
		})
		return
	}

	// Daily finalize: advance to wrapping_up if not already there or
	// finalized, then enqueue the extraction job.
	if session.Phase != domain.ChatPhaseWrappingUp && session.Phase != domain.ChatPhaseFinalized {
		if updated, err := h.Sessions.AdvancePhase(r.Context(), sessionID, domain.ChatPhaseWrappingUp); err == nil {
			session = updated
		}
	}
	// Set extraction_status pending so the polling endpoint reflects
	// the impending run before the worker picks it up.
	if err := h.Sessions.SetExtractionStatus(r.Context(), sessionID, domain.ChatExtractionPending, nil); err != nil {
		h.Logger.Warn("set extraction status pending", "err", err, "session_id", sessionID)
	}
	if _, err := h.Jobs.Schedule(r.Context(), session.ID, session.UserID); err != nil {
		h.Logger.Error("schedule extraction", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "schedule failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"session_id":         session.ID,
		"extraction_status":  domain.ChatExtractionPending,
		"phase":              session.Phase,
		"poll_status_url":    fmt.Sprintf("/api/chat/sessions/%s/extraction/status", session.ID),
	})
}

// ---------- /sessions/:id/extraction/status ----------

type extractionStatusResponse struct {
	Status      string  `json:"status"`
	Error       *string `json:"error,omitempty"`
	FinalizedAt *time.Time `json:"finalized_at,omitempty"`
	Phase       string  `json:"phase"`
}

// ExtractionStatus handles GET /api/chat/sessions/:id/extraction/status.
// Polled by the FE every 2s while pending/running. Returns the current
// status, any error message, and finalized_at so the FE can decide when
// to invalidate caches.
func (h *ChatHandler) ExtractionStatus(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, extractionStatusResponse{
		Status:      session.ExtractionStatus,
		Error:       session.ExtractionError,
		FinalizedAt: session.FinalizedAt,
		Phase:       session.Phase,
	})
}

// ---------- /sessions/:id/reset ----------

// Reset handles POST /api/chat/sessions/:id/reset. Destructive: clears
// the transcript and rolls the phase back to greeting so the user can
// start a fresh conversation. The FE shows a confirmation dialog
// before calling. Saved daily_inputs / journal_entries from prior
// extractions are NOT touched.
func (h *ChatHandler) Reset(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	session, err := h.Sessions.Reset(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("reset chat session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "reset failed")
		return
	}

	// Weekly reset restores the chat to its initial state — i.e. the
	// letter shown as the first assistant turn, phase = exploring.
	// Without this, a reset weekly session would land in greeting phase
	// with an empty transcript, and the FE's auto-opener gate (which
	// expects the seeded letter) would never fire.
	messages := []domain.ChatMessage{}
	if session.Scope == domain.ChatScopeWeekly && session.PeriodStart != nil {
		if weekStart, perr := time.Parse("2006-01-02", *session.PeriodStart); perr == nil {
			body := h.composeWeeklyLetterOpener(r.Context(), session.UserID, weekStart)
			if body != "" {
				row, aerr := h.Messages.Append(r.Context(), store.AppendInput{
					SessionID: session.ID,
					Role:      domain.ChatRoleAssistant,
					Content:   body,
				})
				if aerr != nil {
					h.Logger.Warn("reseed weekly letter on reset", "err", aerr, "session_id", session.ID)
				} else {
					messages = append(messages, *row)
					if updated, perr2 := h.Sessions.AdvancePhase(r.Context(), session.ID, domain.ChatPhaseExploring); perr2 == nil {
						session = updated
					}
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, chatSessionEnvelope{
		Session:  session,
		Messages: messages,
	})
}

// ---------- /sessions/:id/wrap-up ----------

// WrapUp handles POST /api/chat/sessions/:id/wrap-up. The user has
// signaled they're ready to be done; the bot needs to do a fast
// covering pass over any uncovered topics and propose wrap-up.
//
// Wire shape:
//   1. Persist a system_event chat_message ("user_wrap_up") so the
//      LLM transcript includes the signal verbatim and the audit log
//      shows what triggered the closing turn.
//   2. Persist a visible user chat_message ("Let's wrap it up") so the
//      transcript reads as a real user-driven close — the assistant's
//      reply is then a response to the user, not a self-initiated wrap.
//   3. Advance session phase to wrapping_up. The persona prompt's
//      wrapping_up branch instructs the model to ask one final
//      optional question and call propose_wrap_up.
//   4. Stream a single assistant turn via runStream. The FE consumes
//      it with the same SSE handler as a normal message turn.
//
// Idempotent against re-clicks: a second wrap-up just appends another
// system_event + user message (cheap), re-runs runStream, and the
// persona prompt is already in wrapping_up so the bot's response is
// consistent.
func (h *ChatHandler) WrapUp(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	// Need at least one user turn to wrap up — wrap-up against an
	// opener-only chat would be a no-op covering pass with nothing to
	// reflect on. The FE gates the button on hasUserTurns too, so
	// this is a defensive 409.
	existing, err := h.Messages.ListBySession(r.Context(), sessionID)
	if err != nil {
		h.Logger.Error("list messages", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	hasUser := false
	for _, m := range existing {
		if m.Role == domain.ChatRoleUser {
			hasUser = true
			break
		}
	}
	if !hasUser {
		writeJSONError(w, http.StatusConflict, "no user turns yet")
		return
	}

	if _, err := h.Messages.Append(r.Context(), store.AppendInput{
		SessionID: session.ID,
		Role:      domain.ChatRoleSystemEvent,
		Content:   "user_wrap_up",
	}); err != nil {
		h.Logger.Error("append wrap-up event", "err", err, "session_id", session.ID)
		writeJSONError(w, http.StatusInternalServerError, "wrap-up failed")
		return
	}

	if _, err := h.Messages.Append(r.Context(), store.AppendInput{
		SessionID: session.ID,
		Role:      domain.ChatRoleUser,
		Content:   "Let's wrap it up",
	}); err != nil {
		h.Logger.Error("append wrap-up user message", "err", err, "session_id", session.ID)
		writeJSONError(w, http.StatusInternalServerError, "wrap-up failed")
		return
	}

	phaseFrame := ""
	if session.Phase != domain.ChatPhaseWrappingUp {
		updated, err := h.Sessions.AdvancePhase(r.Context(), session.ID, domain.ChatPhaseWrappingUp)
		if err == nil {
			session = updated
			phaseFrame = domain.ChatPhaseWrappingUp
		} else if !errors.Is(err, store.ErrChatSessionInvalidPhase) {
			h.Logger.Warn("advance phase to wrapping_up", "err", err, "session_id", session.ID)
		}
	}

	rc := http.NewResponseController(w)
	h.runStream(w, r, rc, session, false, phaseFrame, nil)
}

// ---------- /sessions/:id/wrap-up/cancel ----------

// CancelWrapUp handles POST /api/chat/sessions/:id/wrap-up/cancel. The
// user opened the kebab and chose "Cancel wrap-up": flip the session
// back to `exploring` so the next assistant turn drops the "land it"
// framing. Records a `user_cancel_wrap_up` system_event so the LLM
// transcript reflects the change of heart on the next turn.
//
// 409 if the session is not currently in `wrapping_up`. No SSE — single
// JSON response, the FE patches its phase cache locally.
func (h *ChatHandler) CancelWrapUp(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	if session.Phase != domain.ChatPhaseWrappingUp {
		writeJSONError(w, http.StatusConflict, "session is not wrapping up")
		return
	}

	if _, err := h.Messages.Append(r.Context(), store.AppendInput{
		SessionID: session.ID,
		Role:      domain.ChatRoleSystemEvent,
		Content:   "user_cancel_wrap_up",
	}); err != nil {
		h.Logger.Error("append cancel-wrap-up event", "err", err, "session_id", session.ID)
		writeJSONError(w, http.StatusInternalServerError, "cancel failed")
		return
	}

	if _, err := h.Sessions.AdvancePhase(r.Context(), session.ID, domain.ChatPhaseExploring); err != nil {
		if !errors.Is(err, store.ErrChatSessionInvalidPhase) {
			h.Logger.Warn("advance phase to exploring", "err", err, "session_id", session.ID)
			writeJSONError(w, http.StatusInternalServerError, "phase update failed")
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"phase": domain.ChatPhaseExploring})
}

// ---------- /sessions/:id/voice/start ----------

type startVoiceResponse struct {
	ClientSecret string `json:"client_secret"`
	ExpiresAt    int64  `json:"expires_at"`
	Model        string `json:"model"`
	SessionID    string `json:"session_id"`
	OpenAISessionID string `json:"openai_session_id"`
}

// StartVoice handles POST /api/chat/sessions/:id/voice/start. Mints an
// OpenAI Realtime ephemeral client_secret using the same composed
// system prompt the text chat uses, persists mode='voice' +
// openai_session_id on the session, and returns the secret to the
// browser. The browser opens the WebRTC peer directly to OpenAI; audio
// never traverses this server.
//
// 503 when OPENAI_API_KEY is unset (dev environments without the key
// still serve text chat). 409 if the session is finalized — voice
// requires an open session.
func (h *ChatHandler) StartVoice(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	if h.Realtime == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "voice not configured")
		return
	}
	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	if session.Phase == domain.ChatPhaseAbandoned {
		writeJSONError(w, http.StatusConflict, "session no longer active")
		return
	}

	user, err := h.Users.GetByID(r.Context(), session.UserID)
	if err != nil || user == nil {
		h.Logger.Error("get user for voice", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	systemPrompt, _, _, err := h.buildSystemPrompt(r.Context(), user, session)
	if err != nil {
		h.Logger.Error("build system prompt for voice", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "prompt build failed")
		return
	}
	// Voice tone tweak + tool override. The base system prompt advertises
	// a propose_wrap_up tool (used in text/SSE mode), but the realtime
	// session has no tools registered — function-call attempts surface
	// as empty `{}` transcript turns. Override here so the model just
	// has a conversation; wrap-up lives in the chat composer after the
	// call ends.
	systemPrompt += "\n\nYou are speaking out loud now — voice mode. Skip headers, " +
		"bullet lists, and any formatting that doesn't read aloud. Keep replies " +
		"short and conversational; let pauses do work. " +
		"Voice mode has NO tools available: ignore the Tools section above. " +
		"Do not call propose_wrap_up or any other tool — never emit a function " +
		"call. When the user signals they're done, just say a short closing " +
		"line and stop talking; the user will end the call themselves."

	out, err := h.Realtime.MintEphemeralSecret(r.Context(), realtime.MintRequest{
		Model:            h.RealtimeModel,
		Instructions:     systemPrompt,
		SafetyIdentifier: hashedSafetyID(user.ID),
	})
	if err != nil {
		if errors.Is(err, realtime.ErrNoAPIKey) {
			writeJSONError(w, http.StatusServiceUnavailable, "voice not configured")
			return
		}
		h.Logger.Error("mint realtime secret", "err", err, "session_id", session.ID)
		writeJSONError(w, http.StatusBadGateway, "voice provider unavailable")
		return
	}

	if err := h.Sessions.MarkVoice(r.Context(), sess.UserID, session.ID, out.SessionID); err != nil {
		h.Logger.Error("mark session voice", "err", err, "session_id", session.ID)
		writeJSONError(w, http.StatusInternalServerError, "session update failed")
		return
	}

	writeJSON(w, http.StatusOK, startVoiceResponse{
		ClientSecret:    out.Value,
		ExpiresAt:       out.ExpiresAt,
		Model:           out.Model,
		SessionID:       session.ID,
		OpenAISessionID: out.SessionID,
	})
}

// ---------- /sessions/:id/voice/transcript ----------

type voiceTranscriptRequest struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// ClientSeq is the browser's monotonic counter; informational only.
	// Server seq is allocated under FOR UPDATE in ChatMessageStore.Append.
	ClientSeq int `json:"client_seq"`
}

type voiceTranscriptResponse struct {
	MessageID string `json:"message_id"`
	Seq       int    `json:"seq"`
}

// AppendVoiceTranscript handles POST /api/chat/sessions/:id/voice/transcript.
// The browser listens to the OpenAI Realtime data channel for finalized
// transcript events (user `*.input_audio_transcription.completed` and
// assistant `*.audio_transcript.done`) and POSTs each turn here so it
// lands in chat_messages. The session shows the same bubbles whether the
// user is in the Talk tab or has switched back to Chat.
//
// 409 when the session is not in voice mode — text chat persists user
// turns through StreamMessage and assistant turns through the SSE
// stream, so a stray POST here would race those writers.
func (h *ChatHandler) AppendVoiceTranscript(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeJSONError(w, http.StatusBadRequest, "id required")
		return
	}
	var req voiceTranscriptRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	role := strings.TrimSpace(req.Role)
	if role != domain.ChatRoleUser && role != domain.ChatRoleAssistant {
		writeJSONError(w, http.StatusBadRequest, "role must be user|assistant")
		return
	}
	content := strings.TrimSpace(req.Content)
	if content == "" {
		writeJSONError(w, http.StatusBadRequest, "content required")
		return
	}
	if len(content) > maxChatContentLen {
		writeJSONError(w, http.StatusBadRequest, "content too long")
		return
	}

	session, err := h.Sessions.GetByID(r.Context(), sess.UserID, sessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			writeJSONError(w, http.StatusNotFound, "session not found")
			return
		}
		h.Logger.Error("get session", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "session lookup failed")
		return
	}
	if session.Mode != domain.ChatModeVoice {
		writeJSONError(w, http.StatusConflict, "session is not in voice mode")
		return
	}

	// Crisis regex applies to user voice too — same safety net the text
	// path runs. On hit we persist the user turn + a system_event so the
	// FE crisis handling re-uses chat_messages history; the FE is also
	// expected to stop the WebRTC call when it sees the system_event
	// land. We don't return crisis details over this channel — the FE
	// already heard the user say it.
	if role == domain.ChatRoleUser && chat.IsCrisis(content) {
		_, _ = h.Messages.Append(r.Context(), store.AppendInput{
			SessionID: session.ID,
			Role:      domain.ChatRoleUser,
			Content:   content,
		})
		_, _ = h.Messages.Append(r.Context(), store.AppendInput{
			SessionID: session.ID,
			Role:      domain.ChatRoleSystemEvent,
			Content:   "crisis_detected",
		})
		_ = h.Sessions.TouchActivity(r.Context(), session.ID)
		writeJSON(w, http.StatusOK, map[string]any{
			"crisis":        true,
			"resources_url": h.ResourcesURL,
		})
		return
	}

	row, err := h.Messages.Append(r.Context(), store.AppendInput{
		SessionID: session.ID,
		Role:      role,
		Content:   content,
	})
	if err != nil {
		h.Logger.Error("append voice transcript", "err", err, "session_id", session.ID)
		writeJSONError(w, http.StatusInternalServerError, "persist failed")
		return
	}
	_ = h.Sessions.TouchActivity(r.Context(), session.ID)

	// First user turn flips greeting → exploring (parity with text chat).
	if role == domain.ChatRoleUser &&
		session.Phase != domain.ChatPhaseExploring &&
		session.Phase != domain.ChatPhaseWrappingUp {
		_, _ = h.Sessions.AdvancePhase(r.Context(), session.ID, domain.ChatPhaseExploring)
	}

	writeJSON(w, http.StatusCreated, voiceTranscriptResponse{
		MessageID: row.ID,
		Seq:       row.Seq,
	})
}

// ---------- shared streaming ----------

// runStream is the inner streaming loop shared by StreamMessage and
// Opener. opener=true skips the user message in the LLM messages
// payload (the system prompt's greeting block instructs the model to
// open the conversation).
//
// `phaseFrameNeeded` is non-empty when the caller already advanced the
// session phase and wants the SSE stream to emit a `phase` frame so
// the FE state syncs.
//
// `userMsg` is non-nil when the caller persisted a user turn; used so
// the assistant content is appended after the user row keeps seq
// monotonic — the lock in Append handles ordering.
func (h *ChatHandler) runStream(
	w http.ResponseWriter,
	r *http.Request,
	rc *http.ResponseController,
	session *domain.ChatSession,
	opener bool,
	phaseFrameNeeded string,
	userMsg *domain.ChatMessage,
) {
	_ = userMsg // signature future-proofs; not needed for ordering today

	// Build prompt context. Failures here are non-fatal at the protocol
	// level — emit an error frame after the SSE handshake so the FE has
	// a structured signal instead of a 500.
	user, err := h.Users.GetByID(r.Context(), session.UserID)
	if err != nil || user == nil {
		writeSSEHeaders(w)
		writeSSEFrame(w, rc, "error", map[string]any{"message": "user lookup failed"})
		return
	}
	systemPrompt, activeGoals, endingGoals, err := h.buildSystemPrompt(r.Context(), user, session)
	if err != nil {
		h.Logger.Error("build system prompt", "err", err)
		writeSSEHeaders(w)
		writeSSEFrame(w, rc, "error", map[string]any{"message": "prompt build failed"})
		return
	}

	transcript, err := h.Messages.LastNForLLM(r.Context(), session.ID, h.KeepLastN)
	if err != nil {
		writeSSEHeaders(w)
		writeSSEFrame(w, rc, "error", map[string]any{"message": "transcript load failed"})
		return
	}
	llmMsgs := chat.MessagesForLLM(transcript)
	maxTokens := chatAssistantMaxTokens
	if opener {
		// Many providers reject empty messages arrays; inject a synthetic
		// priming user turn that's NOT persisted. The system prompt's
		// greeting block governs the actual content.
		llmMsgs = []llm.Message{{Role: "user", Content: "Open the conversation now per the greeting-phase instructions."}}
		maxTokens = chatOpenerMaxTokens
	}

	// Clear any global write deadline so the SSE response can stay open.
	_ = rc.SetWriteDeadline(time.Time{})

	writeSSEHeaders(w)

	model := h.ChatModel
	// Honor a per-session pin, but ignore values that aren't OpenRouter
	// chat-tier models. Older rows may have the realtime model name
	// stamped into chat_model from a prior MarkVoice bug; sending that
	// to OpenRouter 400s. Treat anything matching the realtime model as
	// "not pinned."
	if session.ChatModel != "" && session.ChatModel != h.RealtimeModel {
		model = session.ChatModel
	}
	tools := chat.AssistantTools
	if session.Scope == domain.ChatScopeWeekly {
		tools = chat.WeeklyAssistantTools
	}
	chunks, err := h.ChatLLM.CompleteStream(r.Context(), llm.StreamRequest{
		Model:           model,
		System:          systemPrompt,
		Messages:        llmMsgs,
		Tools:           tools,
		MaxTokens:       maxTokens,
		Temperature:     0.7,
		SystemCacheable: true,
	})
	if err != nil {
		writeSSEFrame(w, rc, "error", map[string]any{"message": err.Error()})
		return
	}

	var fullContent strings.Builder
	var toolCalls []heldToolCall
	var done *llm.StreamDone
	var streamErr error

	for chunk := range chunks {
		if r.Context().Err() != nil {
			// Client disconnected mid-stream — drain remaining chunks
			// to release the channel. Drop on the floor.
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
			toolCalls = append(toolCalls, heldToolCall{
				Name: chunk.ToolCall.Name,
				Args: chunk.ToolCall.Args,
			})
		}
		if chunk.Done != nil {
			done = chunk.Done
		}
	}

	if streamErr != nil {
		writeSSEFrame(w, rc, "error", map[string]any{"message": streamErr.Error()})
		return
	}

	// Persist the assistant turn. Empty content + at least one tool call
	// is allowed (pure tool-call turn); empty content + zero tool calls
	// is treated as a stream that emitted nothing — log + still emit done.
	finalContent := strings.TrimSpace(fullContent.String())
	var firstToolName *string
	var firstToolArgs map[string]any
	if len(toolCalls) > 0 {
		n := toolCalls[0].Name
		firstToolName = &n
		firstToolArgs = toolCalls[0].Args
	}

	// Retry: a propose_wrap_up call is rejected and re-prompted when
	// either (a) the turn has NO accompanying text (breaks the UX —
	// empty bubble), or (b) one or more active goals were never raised
	// in the transcript, which means the model is wrapping up without
	// ever putting those goals to the user. unaddressedGoals uses a
	// permissive keyword heuristic and errs toward "addressed" so the
	// retry only fires when there's high confidence the model skipped
	// the goal entirely. If the goal HAS been raised but the user's
	// answer is ambiguous, that's the model's wrap-up judgment to make
	// (per the wrapping_up phase block in daily_chat_context.tmpl).
	//
	// Weekly extension: the wrap-up gate ALSO requires that every
	// "ending this week" goal has received a propose_extend_goal or
	// propose_complete_goal tool call earlier in the transcript. The
	// FE renders the inline confirmation card from those tool calls;
	// without one the user has no way to settle the goal.
	toolIsWrapUp := firstToolName != nil && *firstToolName == chat.ToolProposeWrapUp
	emptyTurn := finalContent == ""
	var skippedGoals []chat.GoalView
	var undecidedEndingGoals []chat.GoalView
	if toolIsWrapUp {
		if session.Scope == domain.ChatScopeWeekly {
			// Weekly chat doesn't gate on "every goal raised in plain
			// conversation" the way daily does — the gate is "every
			// ENDING goal has a decision tool call." So skipped-goals
			// is only computed for daily.
			undecidedEndingGoals = endingGoalsWithoutDecision(endingGoals, transcript)
		} else {
			skippedGoals = unaddressedGoals(activeGoals, transcript)
		}
	}
	if toolIsWrapUp && (emptyTurn || len(skippedGoals) > 0 || len(undecidedEndingGoals) > 0) {
		var parts []string
		parts = append(parts, "Your previous propose_wrap_up call was rejected. Re-do this turn now.")
		if emptyTurn {
			parts = append(parts, "Issue: the tool call had NO accompanying text. Every assistant turn MUST emit a plain-text reply to the user BEFORE any tool call.")
		}
		if len(skippedGoals) > 0 {
			var lines []string
			for _, g := range skippedGoals {
				lines = append(lines, fmt.Sprintf("- %q (goal: %s)", g.CheckInQuestion, g.Title))
			}
			parts = append(parts, "Issue: these active goal(s) have NOT been put to the user in this session — no yes, no, or explicit refusal in the transcript:\n"+strings.Join(lines, "\n"))
		}
		if len(undecidedEndingGoals) > 0 {
			var lines []string
			for _, g := range undecidedEndingGoals {
				lines = append(lines, fmt.Sprintf("- %s (goal_id: %s)", g.Title, g.ID))
			}
			parts = append(parts, "Issue: these ENDING-THIS-WEEK goal(s) lack a propose_extend_goal OR propose_complete_goal tool call in this transcript. You MUST surface one of those tools (with the user's decision) before you can call propose_wrap_up:\n"+strings.Join(lines, "\n"))
		}
		if session.Scope == domain.ChatScopeWeekly {
			parts = append(parts, "Action: emit a SHORT warm plain-text reply that asks ONE focused open question about the single most pressing missing item. If an ending goal is undecided, ask the user whether they want to extend it or call it done — when they answer clearly, call propose_extend_goal or propose_complete_goal (not propose_wrap_up). Do NOT call propose_wrap_up in this retry; the user is not done.")
		} else {
			parts = append(parts, "Action: emit a SHORT warm plain-text reply that asks ONE focused open question about the single most pressing missing item. Tiebreak: any unanswered goal > drained > charged > grateful. If a goal is unanswered, ask about that goal in plain conversation using the check-in question's own words — do not list multiple goals. Do NOT call propose_wrap_up in this retry; the user is not done.")
		}
		retryNote := strings.Join(parts, "\n\n")
		retryMsgs := append([]llm.Message{}, llmMsgs...)
		retryMsgs = append(retryMsgs, llm.Message{Role: "system", Content: retryNote})
		retryContent, retryToolCalls, retryDone, retryErr := h.streamAssistantOnce(r, w, rc, model, systemPrompt, retryMsgs, maxTokens, session.Scope)
		if retryErr != nil {
			h.Logger.Warn("wrap-up retry failed", "err", retryErr, "session_id", session.ID, "skipped_goals", len(skippedGoals), "undecided_ending", len(undecidedEndingGoals))
			// Fall through with the safety-net text below.
		} else {
			finalContent = retryContent
			toolCalls = retryToolCalls
			if done == nil || retryDone != nil {
				done = retryDone
			}
			firstToolName = nil
			firstToolArgs = nil
			if len(toolCalls) > 0 {
				n := toolCalls[0].Name
				firstToolName = &n
				firstToolArgs = toolCalls[0].Args
			}
		}
	}

	// Safety net: if we still have no text + a tool call (retry also
	// misbehaved, or non-wrap-up tool that produced no text), inject a
	// default sign-off so the user always sees something visible. Also
	// streamed as a token frame so it animates in.
	if finalContent == "" && firstToolName != nil {
		fallback := defaultToolFallbackText(*firstToolName)
		if fallback != "" {
			finalContent = fallback
			writeSSEFrame(w, rc, "token", map[string]any{"delta": fallback})
		}
	}
	var asstID string
	var asstCreatedAt time.Time
	if finalContent != "" || firstToolName != nil {
		tokenIn, tokenOut := 0, 0
		modelName := model
		if done != nil {
			tokenIn = done.PromptTokens
			tokenOut = done.CompletionTokens
			if done.Model != "" {
				modelName = done.Model
			}
		}
		_ = modelName // currently unused; reserved for future per-row model audit
		row, err := h.Messages.Append(r.Context(), store.AppendInput{
			SessionID: session.ID,
			Role:      domain.ChatRoleAssistant,
			Content:   finalContent,
			ToolName:  firstToolName,
			ToolArgs:  firstToolArgs,
			TokenIn:   tokenIn,
			TokenOut:  tokenOut,
		})
		if err != nil {
			h.Logger.Error("append assistant message", "err", err, "session_id", session.ID)
		} else {
			asstID = row.ID
			asstCreatedAt = row.CreatedAt
		}
		_ = h.Sessions.TouchActivity(r.Context(), session.ID)
	}

	// Phase-frame emission: greeting → exploring transitions on first
	// user turn (set by StreamMessage), and propose_wrap_up flips to
	// wrapping_up here.
	if phaseFrameNeeded != "" {
		writeSSEFrame(w, rc, "phase", map[string]any{"phase": phaseFrameNeeded})
	}

	// Fan out tool calls beyond the first as event:tool frames. The
	// first one is also surfaced (uniformly) so the FE doesn't have
	// special-cased "first tool is in the assistant row" logic.
	for _, tc := range toolCalls {
		writeSSEFrame(w, rc, "tool", map[string]any{
			"name": tc.Name,
			"args": tc.Args,
		})
		if tc.Name == chat.ToolProposeWrapUp && session.Phase != domain.ChatPhaseWrappingUp {
			if _, err := h.Sessions.AdvancePhase(r.Context(), session.ID, domain.ChatPhaseWrappingUp); err == nil {
				writeSSEFrame(w, rc, "phase", map[string]any{"phase": domain.ChatPhaseWrappingUp})
			}
		}
	}

	if opener {
		// Greeting → exploring transition is only triggered by a user
		// reply (StreamMessage advances). The opener stays in greeting
		// phase until the first user turn; the FE shows the streamed
		// greeting and the composer is enabled awaiting input.
	}

	// Emit `done` BEFORE running the coverage classifier so the FE can
	// re-enable the composer immediately — the classifier is the slow
	// part of the turn and is advisory; the user shouldn't wait on it
	// to send the next message.
	donePayload := map[string]any{
		"assistant_message_id": asstID,
		"finish_reason":        "",
	}
	if asstCreatedAt.IsZero() == false {
		donePayload["created_at"] = asstCreatedAt
	}
	if done != nil {
		donePayload["prompt_tokens"] = done.PromptTokens
		donePayload["completion_tokens"] = done.CompletionTokens
		donePayload["finish_reason"] = done.FinishReason
		donePayload["model"] = done.Model
	}
	writeSSEFrame(w, rc, "done", donePayload)

	// Post-turn coverage classifier intentionally disabled: the model
	// itself decides on the wrap-up turn what topics are still missing
	// (see the wrapping_up branch in daily_chat_context.tmpl). Saves an
	// LLM call per turn and removes a moving piece. runCoverageClassifier
	// is left in place dormant in case we re-enable later.
}

// hashedSafetyID returns a 16-hex-char prefix of sha256(userID), stable
// per-user but opaque, suitable for OpenAI-Safety-Identifier.
func hashedSafetyID(userID string) string {
	sum := sha256.Sum256([]byte(userID))
	return hex.EncodeToString(sum[:])[:16]
}

// goalKeywordStopwords are short common words excluded when extracting
// distinctive keywords from a goal's check-in question or title. The
// unaddressed-goal heuristic searches the transcript for any of the
// remaining keywords to decide whether the model ever raised the goal.
var goalKeywordStopwords = map[string]bool{
	"what": true, "when": true, "where": true, "which": true, "have": true,
	"your": true, "today": true, "this": true, "that": true, "with": true,
	"into": true, "from": true, "been": true, "were": true, "will": true,
	"just": true, "they": true, "them": true, "than": true, "then": true,
	"some": true, "more": true, "much": true, "many": true, "could": true,
	"would": true, "should": true, "about": true, "after": true, "before": true,
	"while": true, "since": true, "until": true, "even": true, "ever": true,
	"also": true, "very": true, "really": true, "going": true, "didn": true,
	"doing": true, "done": true, "yourself": true,
}

// goalKeywords pulls case-folded distinctive words from a goal's
// check-in question and title for use in the unaddressed-goal
// heuristic. Words ≤ 3 chars and stopwords are dropped.
func goalKeywords(g chat.GoalView) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range strings.Fields(g.CheckInQuestion + " " + g.Title) {
		w := strings.ToLower(strings.TrimFunc(raw, func(r rune) bool {
			return !unicode.IsLetter(r)
		}))
		if len(w) < 4 || goalKeywordStopwords[w] || seen[w] {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

// endingGoalsWithoutDecision returns the subset of goals whose end_date
// has passed (so they appeared in the "Ending this week" section of the
// weekly system prompt) but for which the transcript contains NO
// propose_extend_goal or propose_complete_goal tool call addressing the
// goal_id. Used to gate weekly propose_wrap_up.
//
// Heuristic: scan assistant rows for tool_name == propose_extend_goal
// or propose_complete_goal and check the args' goal_id matches. The
// `activeGoals` slice passed in is the full set (mid-flight + ending);
// we re-detect "ending" by absence-of-keyword-match in the transcript,
// which isn't reliable — instead use the persisted end_date check at
// build-system-prompt time. As a simpler proxy here we just take every
// goal in `endingGoals` (see callers) — this helper assumes the caller
// already filtered to ending goals. For the daily scope path it's
// called with an empty slice and returns nil.
func endingGoalsWithoutDecision(endingGoals []chat.GoalView, transcript []domain.ChatMessage) []chat.GoalView {
	if len(endingGoals) == 0 {
		return nil
	}
	decided := map[string]struct{}{}
	for _, m := range transcript {
		if m.Role != domain.ChatRoleAssistant || m.ToolName == nil {
			continue
		}
		name := *m.ToolName
		if name != chat.ToolProposeExtendGoal && name != chat.ToolProposeCompleteGoal {
			continue
		}
		if m.ToolArgs == nil {
			continue
		}
		if gid, ok := m.ToolArgs["goal_id"].(string); ok && gid != "" {
			decided[gid] = struct{}{}
		}
	}
	var out []chat.GoalView
	for _, g := range endingGoals {
		if _, ok := decided[g.ID]; !ok {
			out = append(out, g)
		}
	}
	return out
}

// unaddressedGoals returns the subset of active goals that appear to
// have never been raised in the transcript. Heuristic: a goal counts
// as addressed if ANY of its distinctive keywords (from
// CheckInQuestion + Title) appears anywhere in any prior message —
// user, assistant, or system_event. Errs toward "addressed" to avoid
// spurious retries; the prompt-level wrap-up rules handle the model's
// answer-clarity judgment when the goal HAS been raised.
func unaddressedGoals(goals []chat.GoalView, transcript []domain.ChatMessage) []chat.GoalView {
	if len(goals) == 0 {
		return nil
	}
	var haystack strings.Builder
	for _, m := range transcript {
		haystack.WriteString(strings.ToLower(m.Content))
		haystack.WriteByte(' ')
	}
	text := haystack.String()
	var out []chat.GoalView
	for _, g := range goals {
		kws := goalKeywords(g)
		if len(kws) == 0 {
			// No distinctive keywords to search on; trust the model.
			continue
		}
		addressed := false
		for _, kw := range kws {
			if strings.Contains(text, kw) {
				addressed = true
				break
			}
		}
		if !addressed {
			out = append(out, g)
		}
	}
	return out
}

// defaultToolFallbackText returns a short visible reply to use when
// the model emitted a tool call without any accompanying text. Keeps
// the user from staring at an empty bubble. Empty string ⇒ no
// injection (the tool truly produces no user-facing message).
func defaultToolFallbackText(toolName string) string {
	switch toolName {
	case chat.ToolProposeWrapUp:
		return "Thanks for sharing all that. Whenever you're ready, you can finish the check-in."
	}
	return ""
}

// heldToolCall captures a tool call emitted by the assistant stream.
// Lifted to package scope so retry helpers can return it.
type heldToolCall struct {
	Name string
	Args map[string]any
}

// streamAssistantOnce runs a single LLM completion stream and
// surfaces token deltas as SSE token frames the same way the inline
// loop in runStream does. Returns the accumulated content, tool
// calls, and the trailing done payload. Errors mid-stream surface as
// a non-nil error (the caller decides whether to write an SSE error
// frame or fall back). On client disconnect the helper drains and
// returns nil error with empty content — the caller can treat as
// "no result."
func (h *ChatHandler) streamAssistantOnce(
	r *http.Request,
	w http.ResponseWriter,
	rc *http.ResponseController,
	model, systemPrompt string,
	messages []llm.Message,
	maxTokens int,
	scope string,
) (string, []heldToolCall, *llm.StreamDone, error) {
	tools := chat.AssistantTools
	if scope == domain.ChatScopeWeekly {
		tools = chat.WeeklyAssistantTools
	}
	chunks, err := h.ChatLLM.CompleteStream(r.Context(), llm.StreamRequest{
		Model:           model,
		System:          systemPrompt,
		Messages:        messages,
		Tools:           tools,
		MaxTokens:       maxTokens,
		Temperature:     0.7,
		SystemCacheable: true,
	})
	if err != nil {
		return "", nil, nil, err
	}
	var content strings.Builder
	var calls []heldToolCall
	var done *llm.StreamDone
	for chunk := range chunks {
		if r.Context().Err() != nil {
			for range chunks {
			}
			return "", nil, nil, nil
		}
		if chunk.Err != nil {
			return "", nil, nil, chunk.Err
		}
		if chunk.Delta != "" {
			content.WriteString(chunk.Delta)
			if !writeSSEFrame(w, rc, "token", map[string]any{"delta": chunk.Delta}) {
				return "", nil, nil, nil
			}
		}
		if chunk.ToolCall != nil {
			calls = append(calls, heldToolCall{
				Name: chunk.ToolCall.Name,
				Args: chunk.ToolCall.Args,
			})
		}
		if chunk.Done != nil {
			done = chunk.Done
		}
	}
	return strings.TrimSpace(content.String()), calls, done, nil
}

// runCoverageClassifier loads the latest transcript + active questions
// and asks the classifier which NEW questions became covered in the
// latest turn(s). The classifier runs in delta mode: it sees the
// previously-covered set as state and returns only the additions; the
// store layer persists the union.
//
// Short-circuit: if no new user message has arrived since the watermark
// (chat_sessions.coverage_last_classified_seq), skip the LLM call —
// the answer can't change. Same set of user words ⇒ same covered set.
//
// All errors are logged and swallowed — coverage is advisory; the
// stream continues regardless. Called from runStream after `done`.
func (h *ChatHandler) runCoverageClassifier(
	ctx context.Context,
	w http.ResponseWriter,
	rc *http.ResponseController,
	session *domain.ChatSession,
) {
	transcript, err := h.Messages.ListBySession(ctx, session.ID)
	if err != nil {
		h.Logger.Warn("coverage: load transcript", "err", err, "session_id", session.ID)
		return
	}
	maxUserSeq := 0
	for _, m := range transcript {
		if m.Role == domain.ChatRoleUser && m.Seq > maxUserSeq {
			maxUserSeq = m.Seq
		}
	}
	if maxUserSeq == 0 {
		// No user turns — opener-only state. Nothing to classify.
		return
	}
	if maxUserSeq <= session.CoverageLastClassifiedSeq {
		// Same user-side input as the last successful classification.
		// The covered set is sticky; nothing to recompute.
		return
	}
	classifyModel := h.ClassifyModel
	if session.ExtractionModel != "" {
		classifyModel = session.ExtractionModel
	}
	covered, err := chat.Classify(ctx, h.ClassifyLLM, chat.CoverageParams{
		Model:             classifyModel,
		Messages:          transcript,
		PreviouslyCovered: session.CoveredQuestionIDs,
	})
	if err != nil {
		h.Logger.Warn("coverage: classify", "err", err, "session_id", session.ID)
		return
	}
	if covered == nil {
		// Greeting-only path — no user turns yet. Leave the persisted
		// value alone and skip the SSE frame.
		return
	}
	if err := h.Sessions.SetCoveredQuestionIDs(ctx, session.ID, covered, maxUserSeq); err != nil {
		h.Logger.Warn("coverage: persist", "err", err, "session_id", session.ID)
	}
	writeSSEFrame(w, rc, "coverage_update", map[string]any{
		"covered_question_ids": covered,
	})
}

// buildSystemPrompt assembles the session's system prompt — branching
// on session.Scope to either the daily Energy-Audit persona or the
// weekly reflection persona. Returns the rendered prompt plus two goal
// slices:
//
//   - allActive: every active goal (mid-flight + ending). Daily wrap-up
//     gating uses this directly (every active goal must be put to the
//     user). Voice mode ignores it.
//   - endingThisWeek: the subset whose end_date is on or before the
//     week_end of a weekly session. Empty for daily scope. Used by the
//     weekly wrap-up gate to enforce that each ending goal has received
//     a propose_extend_goal or propose_complete_goal tool call.
func (h *ChatHandler) buildSystemPrompt(
	ctx context.Context, user *domain.User, session *domain.ChatSession,
) (string, []chat.GoalView, []chat.GoalView, error) {
	if session.Scope == domain.ChatScopeWeekly {
		return h.buildWeeklySystemPrompt(ctx, user, session)
	}
	return h.buildDailySystemPrompt(ctx, user, session)
}

// buildDailySystemPrompt is the original daily/Energy-Audit branch.
func (h *ChatHandler) buildDailySystemPrompt(
	ctx context.Context, user *domain.User, session *domain.ChatSession,
) (string, []chat.GoalView, []chat.GoalView, error) {
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	nowLocal := time.Now().In(loc)

	questions, err := h.Questions.ListActive(ctx, user.ID)
	if err != nil {
		return "", nil, nil, fmt.Errorf("load questions: %w", err)
	}
	views := chat.QuestionViewsFromDomain(questions)

	// Active goals scoped to the session's journal date (post day-start
	// cutoff) — consumed by both text chat (this prompt) and voice
	// (StartVoice reuses buildSystemPrompt before minting the realtime
	// secret), so a single load covers both surfaces.
	var goalViews []chat.GoalView
	if h.Goals != nil {
		if asOf, perr := time.Parse("2006-01-02", session.LocalDate); perr == nil {
			activeGoals, gerr := h.Goals.ListActive(ctx, user.ID, asOf)
			if gerr != nil {
				return "", nil, nil, fmt.Errorf("load goals: %w", gerr)
			}
			goalViews = chat.GoalViewsFromDomain(activeGoals)
		}
	}

	// Recent context: 7-day window ending yesterday (today excluded).
	localToday, err := timezone.LocalDate(time.Now(), user.Timezone, user.DayStartMinutes)
	if err != nil {
		return "", nil, nil, err
	}
	since := localToday.AddDate(0, 0, -7)
	until := localToday.AddDate(0, 0, -1)
	var moodAvg *float64
	// Emotions are retired under the Energy Audit pivot — Phase 3 will
	// swap RecentTopEmotions for a "recent drainers/chargers" view.
	var topEmotions []string
	if !until.Before(since) {
		if agg, err := h.DailyInputs.AggregateForRange(ctx, user.ID, since, until); err == nil && agg != nil {
			moodAvg = agg.MoodScore
		}
	}

	displayName := ""
	if user.DisplayName != nil {
		displayName = *user.DisplayName
	}

	// Journal-date weekday must come from session.LocalDate, NOT
	// nowLocal — after midnight before the day-start cutoff they
	// disagree (session.LocalDate = yesterday, nowLocal = today).
	journalWeekday := ""
	if jd, err := time.ParseInLocation("2006-01-02", session.LocalDate, loc); err == nil {
		journalWeekday = jd.Weekday().String()
	}
	dsm := user.DayStartMinutes
	if dsm < 0 {
		dsm = 0
	}
	dayStartLabel := fmt.Sprintf("%02d:%02d", dsm/60, dsm%60)

	prompt, err := chat.BuildSystemPrompt(chat.BuildSystemPromptParams{
		DisplayName:       displayName,
		JournalDate:       session.LocalDate,
		JournalWeekday:    journalWeekday,
		WallClockDate:     nowLocal.Format("2006-01-02"),
		WallClockWeekday:  nowLocal.Weekday().String(),
		WallClockTime:     nowLocal.Format("15:04"),
		DayStartLabel:     dayStartLabel,
		LocalTimeOfDay:    chat.TimeOfDay(nowLocal),
		Questions:         views,
		Goals:             goalViews,
		Recent7DayMoodAvg: moodAvg,
		RecentTopEmotions: topEmotions,
		Phase:             session.Phase,
		HardCapMinutes:    h.HardCapMinutes,
	})
	if err != nil {
		return "", nil, nil, err
	}
	return prompt, goalViews, nil, nil
}

// buildWeeklySystemPrompt assembles the weekly-reflection chat's system
// prompt. Inputs: the letter (loaded from summaries.metadata), active
// goals split by end_date, top drainers/chargers for the week, and the
// prior-week surprise sentence for continuity. session.PeriodStart is
// the week_start anchor.
func (h *ChatHandler) buildWeeklySystemPrompt(
	ctx context.Context, user *domain.User, session *domain.ChatSession,
) (string, []chat.GoalView, []chat.GoalView, error) {
	if session.PeriodStart == nil {
		return "", nil, nil, fmt.Errorf("weekly session has no period_start")
	}
	weekStart, err := time.Parse("2006-01-02", *session.PeriodStart)
	if err != nil {
		return "", nil, nil, fmt.Errorf("parse period_start: %w", err)
	}
	weekEnd := weekStart.AddDate(0, 0, 6)

	displayName := ""
	if user.DisplayName != nil {
		displayName = *user.DisplayName
	}

	// Letter. Pull the summaries row for this week_start; fall back to
	// the most recent weekly summary up to weekEnd so legacy-anchored
	// rows still seed the prompt with something.
	var letter chat.WeeklyLetterView
	if h.Summaries != nil {
		summary, _ := h.Summaries.GetByPeriod(ctx, user.ID, string(domain.PeriodWeek), weekStart)
		if summary == nil || !summary.Metadata.HasLetterSynthesis() {
			if latest, _ := h.Summaries.LatestByPeriodTypeUpTo(ctx, user.ID, string(domain.PeriodWeek), weekEnd); latest != nil && latest.Metadata.HasLetterSynthesis() {
				summary = latest
			}
		}
		if summary != nil {
			letter = chat.WeeklyLetterView{
				Charged:         summary.Metadata.Charged,
				Drained:         summary.Metadata.Drained,
				Grateful:        summary.Metadata.Grateful,
				Insights:        summary.Metadata.Insights,
				ClosingQuestion: summary.Metadata.ClosingQuestion,
			}
		}
	}

	// Patterns: top 3 drainers + chargers for the 7-day window.
	var topDrainers, topChargers []chat.TagSummary
	if h.DailyEntryTags != nil {
		drainAggs, _ := h.DailyEntryTags.TopByValence(ctx, user.ID, domain.TagRoleDrainer, 7, 3)
		for _, a := range drainAggs {
			topDrainers = append(topDrainers, chat.TagSummary{Label: a.Label, Appearances: a.Appearances})
		}
		chargeAggs, _ := h.DailyEntryTags.TopByValence(ctx, user.ID, domain.TagRoleCharger, 7, 3)
		for _, a := range chargeAggs {
			topChargers = append(topChargers, chat.TagSummary{Label: a.Label, Appearances: a.Appearances})
		}
	}

	// Goals: split into mid-flight (end_date > weekEnd) and ending
	// (end_date <= weekEnd). We list active as-of weekEnd so we don't
	// miss a goal that ends exactly on weekEnd.
	var allActive, endingGoals, midGoals []chat.GoalView
	if h.Goals != nil {
		rawGoals, gerr := h.Goals.ListActive(ctx, user.ID, weekEnd)
		if gerr != nil {
			return "", nil, nil, fmt.Errorf("load goals: %w", gerr)
		}
		for _, g := range rawGoals {
			view := chat.GoalView{ID: g.ID, Title: g.Title, CheckInQuestion: g.CheckInQuestion}
			allActive = append(allActive, view)
			endDate, perr := time.Parse("2006-01-02", g.EndDate)
			if perr != nil {
				midGoals = append(midGoals, view)
				continue
			}
			if !endDate.After(weekEnd) {
				endingGoals = append(endingGoals, view)
			} else {
				midGoals = append(midGoals, view)
			}
		}
	}

	// Continuity: previous week's distilled surprise.
	prevSurprise := ""
	if h.WeeklyReflections != nil {
		if prev, _ := h.WeeklyReflections.LatestBeforeWeek(ctx, user.ID, weekStart); prev != nil {
			prevSurprise = prev.SurpriseText
		}
	}

	prompt, err := chat.BuildWeeklySystemPrompt(chat.BuildWeeklySystemPromptParams{
		DisplayName:      displayName,
		WeekStart:        timezone.FormatDate(weekStart),
		WeekEnd:          timezone.FormatDate(weekEnd),
		Letter:           letter,
		TopDrainers:      topDrainers,
		TopChargers:      topChargers,
		MidFlightGoals:   midGoals,
		EndingGoals:      endingGoals,
		PrevWeekSurprise: prevSurprise,
		Phase:            session.Phase,
	})
	if err != nil {
		return "", nil, nil, err
	}
	return prompt, allActive, endingGoals, nil
}
