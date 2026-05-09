package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/chat"
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
	Users          *store.UserStore
	DailyInputs    *store.DailyInputStore
	ChatLLM        *llm.OpenRouter
	ClassifyLLM    *llm.OpenRouter
	Logger         *slog.Logger
	ChatModel      string
	ClassifyModel  string
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
	// No phase-based blocking. Chat stays open per (user, local_date) all
	// day; finalize is just a "refresh the check-in now" trigger, not a
	// lock. If the session was previously finalized, the first new user
	// turn rolls phase back to exploring (handled below).

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
	//   - wrapping_up → exploring (user keeps typing after the model
	//     proposed wrap-up)
	//   - finalized → exploring (user resumes after a previous extraction)
	// The phase frame syncs the FE so its "wrap up?" affordance hides
	// and the composer state matches.
	phaseFrameNeeded := ""
	if session.Phase != domain.ChatPhaseExploring {
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
// double-clicks within the same minute return the same job.
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
	// Advance to wrapping_up if not already there or finalized.
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
	writeJSON(w, http.StatusOK, chatSessionEnvelope{
		Session:  session,
		Messages: []domain.ChatMessage{},
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
//   2. Advance session phase to wrapping_up. The persona prompt's
//      wrapping_up branch instructs the model to ask one final
//      optional question and call propose_wrap_up.
//   3. Stream a single assistant turn via runStream. The FE consumes
//      it with the same SSE handler as a normal message turn.
//
// Idempotent against re-clicks: a second wrap-up just appends another
// system_event (cheap), re-runs runStream, and the persona prompt is
// already in wrapping_up so the bot's response is consistent.
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
	systemPrompt, err := h.buildSystemPrompt(r.Context(), user, session)
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
	if session.ChatModel != "" {
		model = session.ChatModel
	}
	chunks, err := h.ChatLLM.CompleteStream(r.Context(), llm.StreamRequest{
		Model:           model,
		System:          systemPrompt,
		Messages:        llmMsgs,
		Tools:           chat.AssistantTools,
		MaxTokens:       maxTokens,
		Temperature:     0.7,
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

	// Safety net: a tool-call-only turn (e.g. the model called
	// propose_wrap_up without text) leaves the user staring at an empty
	// bubble that visibleMessages filters out. Inject a default sign-off
	// so the user always sees something. We also stream it as a token
	// frame so the UI animates it like any other reply rather than
	// snapping in on refetch.
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

// buildSystemPrompt assembles the session's BuildSystemPromptParams and
// renders the chat system prompt. Recent context is pulled from the
// last 7 days of daily_inputs (excluding today so today's check-in
// doesn't echo back at the user).
func (h *ChatHandler) buildSystemPrompt(
	ctx context.Context, user *domain.User, session *domain.ChatSession,
) (string, error) {
	loc, err := time.LoadLocation(user.Timezone)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	nowLocal := time.Now().In(loc)

	questions, err := h.Questions.ListActive(ctx, user.ID)
	if err != nil {
		return "", fmt.Errorf("load questions: %w", err)
	}
	views := chat.QuestionViewsFromDomain(questions)

	// Recent context: 7-day window ending yesterday (today excluded).
	localToday, err := timezone.LocalDate(time.Now(), user.Timezone, user.DayStartMinutes)
	if err != nil {
		return "", err
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

	return chat.BuildSystemPrompt(chat.BuildSystemPromptParams{
		DisplayName:       displayName,
		JournalDate:       session.LocalDate,
		JournalWeekday:    journalWeekday,
		WallClockDate:     nowLocal.Format("2006-01-02"),
		WallClockWeekday:  nowLocal.Weekday().String(),
		WallClockTime:     nowLocal.Format("15:04"),
		DayStartLabel:     dayStartLabel,
		LocalTimeOfDay:    chat.TimeOfDay(nowLocal),
		Questions:         views,
		Recent7DayMoodAvg: moodAvg,
		RecentTopEmotions: topEmotions,
		Phase:             session.Phase,
		HardCapMinutes:    h.HardCapMinutes,
	})
}
