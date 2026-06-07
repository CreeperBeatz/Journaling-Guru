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

// MemoryHandler hosts /api/memories — the management surface for the
// durable-fact list. All routes sit behind RequireAuth; user_id scoping
// happens in the store. Every write here is user-authored: Create and
// Update stamp pinned=true server-side, locking the row against the
// reconciliation worker (manual-wins).
type MemoryHandler struct {
	Memories *store.MemoryStore
	Logger   *slog.Logger
}

const maxMemoryContentLen = 500

// List returns the user's active memories, flat with a category field —
// the FE groups them. Superseded/deleted history is not surfaced.
func (h *MemoryHandler) List(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	rows, err := h.Memories.ListActive(r.Context(), sess.UserID)
	if err != nil {
		h.Logger.Error("list memories", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "list failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"memories": rows})
}

type memoryWriteRequest struct {
	Category string `json:"category"`
	Content  string `json:"content"`
}

func (req *memoryWriteRequest) validate() string {
	req.Category = strings.TrimSpace(req.Category)
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		return "content required"
	}
	if len(req.Content) > maxMemoryContentLen {
		return "content too long"
	}
	if !domain.IsValidMemoryCategory(req.Category) {
		return "invalid category"
	}
	return ""
}

// Create makes a user-authored memory (source='user', pinned=true).
func (h *MemoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req memoryWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := req.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}
	m, err := h.Memories.Create(r.Context(), sess.UserID, req.Category, req.Content)
	if err != nil {
		h.Logger.Error("create memory", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, m)
}

// Update handles PATCH /api/memories/:id — edit content/category. The
// store flips the row to pinned=true / source='user' so an edited fact
// can never be clobbered by the next reconciliation pass.
func (h *MemoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := chi.URLParam(r, "id")
	var req memoryWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if msg := req.validate(); msg != "" {
		writeJSONError(w, http.StatusBadRequest, msg)
		return
	}
	m, err := h.Memories.UpdateContentUser(r.Context(), sess.UserID, id, req.Category, req.Content)
	if err != nil {
		if errors.Is(err, store.ErrMemoryNotFound) {
			writeJSONError(w, http.StatusNotFound, "memory not found")
			return
		}
		h.Logger.Error("update memory", "err", err, "memory_id", id)
		writeJSONError(w, http.StatusInternalServerError, "update failed")
		return
	}
	writeJSON(w, http.StatusOK, m)
}

// Delete handles DELETE /api/memories/:id — soft-delete (history row is
// kept, the fact disappears from prompts and the list).
func (h *MemoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.Memories.SoftDelete(r.Context(), sess.UserID, id); err != nil {
		if errors.Is(err, store.ErrMemoryNotFound) {
			writeJSONError(w, http.StatusNotFound, "memory not found")
			return
		}
		h.Logger.Error("delete memory", "err", err, "memory_id", id)
		writeJSONError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
