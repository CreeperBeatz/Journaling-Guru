package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// QuestionHandler hosts /api/questions. All routes sit behind RequireAuth,
// so SessionFromCtx is non-nil. user_id scoping is enforced inside the
// store, not here.
type QuestionHandler struct {
	Questions *store.QuestionStore
	Logger    *slog.Logger
}

const (
	maxQuestionPromptLen = 500
	maxQuestionsPerUser  = 50
)

// List returns the user's active questions. Seeds defaults on first call
// (so a brand-new user lands on a populated DailyEntry without an
// onboarding step).
func (h *QuestionHandler) List(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	rows, err := h.Questions.ListActive(r.Context(), sess.UserID)
	if err != nil {
		h.Logger.Error("list questions", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	if len(rows) == 0 {
		seeded, err := h.Questions.SeedDefaults(r.Context(), sess.UserID, domain.DefaultQuestions)
		if err != nil {
			// Lost a race with another tab — re-list and serve whatever
			// is now there, instead of failing the request.
			h.Logger.Warn("seed defaults; re-listing", "err", err, "user_id", sess.UserID)
			rows, err = h.Questions.ListActive(r.Context(), sess.UserID)
			if err != nil {
				writeJSONError(w, http.StatusInternalServerError, "list failed")
				return
			}
		} else {
			rows = seeded
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"questions": rows})
}

type createQuestionRequest struct {
	Prompt string `json:"prompt"`
}

// Create appends a new question. 400 when prompt is empty/too long; 409
// when the user already hit the per-account cap.
func (h *QuestionHandler) Create(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req createQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt required")
		return
	}
	if len(prompt) > maxQuestionPromptLen {
		writeJSONError(w, http.StatusBadRequest, "prompt too long")
		return
	}

	existing, err := h.Questions.ListActive(r.Context(), sess.UserID)
	if err != nil {
		h.Logger.Error("list before create", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	if len(existing) >= maxQuestionsPerUser {
		writeJSONError(w, http.StatusConflict, "too many questions")
		return
	}

	q, err := h.Questions.Create(r.Context(), sess.UserID, prompt)
	if err != nil {
		h.Logger.Error("create question", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, q)
}

type updateQuestionRequest struct {
	Prompt string `json:"prompt"`
}

// Update edits the prompt text. 404 if the question isn't the caller's,
// is archived, or doesn't exist.
func (h *QuestionHandler) Update(w http.ResponseWriter, r *http.Request) {
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
	var req updateQuestionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		writeJSONError(w, http.StatusBadRequest, "prompt required")
		return
	}
	if len(prompt) > maxQuestionPromptLen {
		writeJSONError(w, http.StatusBadRequest, "prompt too long")
		return
	}
	q, err := h.Questions.UpdatePrompt(r.Context(), sess.UserID, id, prompt)
	if err != nil {
		if errors.Is(err, store.ErrQuestionNotFound) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		h.Logger.Error("update question", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, q)
}

// Archive soft-deletes the question. Returns 404 when not the caller's.
func (h *QuestionHandler) Archive(w http.ResponseWriter, r *http.Request) {
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
	if err := h.Questions.Archive(r.Context(), sess.UserID, id); err != nil {
		if errors.Is(err, store.ErrQuestionNotFound) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		h.Logger.Error("archive question", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "archive failed")
		return
	}
	writeJSON(w, http.StatusOK, ackResponse{OK: true})
}

type reorderRequest struct {
	IDs []string `json:"ids"`
}

// Reorder rewrites positions to match the provided id list. Every id
// must be active and owned by the caller — otherwise the whole tx is
// rejected with 404 (no partial reorders).
func (h *QuestionHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req reorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.IDs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "ids required")
		return
	}
	if err := h.Questions.Reorder(r.Context(), sess.UserID, req.IDs); err != nil {
		if errors.Is(err, store.ErrQuestionNotFound) {
			writeJSONError(w, http.StatusNotFound, "not found")
			return
		}
		h.Logger.Error("reorder questions", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "reorder failed")
		return
	}
	writeJSON(w, http.StatusOK, ackResponse{OK: true})
}
