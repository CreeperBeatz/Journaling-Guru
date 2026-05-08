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

// TagHandler hosts /api/tags. All routes sit behind RequireAuth so
// SessionFromCtx is non-nil. user_id scoping happens in the store.
type TagHandler struct {
	Tags   *store.TagStore
	Logger *slog.Logger
}

const maxTagLabelLen = 80

// List returns the user's active tags, optionally filtered by valence.
// `?valence=positive|negative|neutral` narrows the picker; omitted
// returns every active tag.
func (h *TagHandler) List(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	valence := strings.TrimSpace(r.URL.Query().Get("valence"))
	if valence != "" && !validTagValence(valence) {
		writeJSONError(w, http.StatusBadRequest, "invalid valence")
		return
	}

	var rows []domain.Tag
	var err error
	if valence == "" {
		// Two queries beats a slightly more complex store API for now —
		// the picker always knows the side it's filling, so the empty
		// case is mostly debug.
		pos, perr := h.Tags.ListActiveByValence(r.Context(), sess.UserID, domain.TagValencePositive)
		if perr != nil {
			h.Logger.Error("list tags pos", "err", perr)
			writeJSONError(w, http.StatusInternalServerError, "list failed")
			return
		}
		neg, nerr := h.Tags.ListActiveByValence(r.Context(), sess.UserID, domain.TagValenceNegative)
		if nerr != nil {
			h.Logger.Error("list tags neg", "err", nerr)
			writeJSONError(w, http.StatusInternalServerError, "list failed")
			return
		}
		rows = append(append(rows, pos...), neg...)
	} else {
		rows, err = h.Tags.ListActiveByValence(r.Context(), sess.UserID, valence)
		if err != nil {
			h.Logger.Error("list tags", "err", err, "valence", valence)
			writeJSONError(w, http.StatusInternalServerError, "list failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": rows})
}

type createTagRequest struct {
	Label   string `json:"label"`
	Valence string `json:"valence"`
}

// Create makes a tag (or returns the existing matching tag, since
// UpsertByLabel is idempotent on normalized_label). Used by the
// Manual-tab "add new" affordance and by callers that want a tag ID
// before linking a day.
func (h *TagHandler) Create(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req createTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		writeJSONError(w, http.StatusBadRequest, "label required")
		return
	}
	if len(label) > maxTagLabelLen {
		writeJSONError(w, http.StatusBadRequest, "label too long")
		return
	}
	if !validTagValence(req.Valence) {
		writeJSONError(w, http.StatusBadRequest, "invalid valence")
		return
	}
	tag, err := h.Tags.UpsertByLabel(r.Context(), sess.UserID, label, req.Valence)
	if err != nil {
		h.Logger.Error("upsert tag", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "create failed")
		return
	}
	writeJSON(w, http.StatusCreated, tag)
}

type renameTagRequest struct {
	Label string `json:"label"`
}

// Update handles PATCH /api/tags/:id — rename. Returns 409 if a
// different tag already owns the new normalized label (caller should
// offer to merge instead).
func (h *TagHandler) Update(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := chi.URLParam(r, "id")
	var req renameTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		writeJSONError(w, http.StatusBadRequest, "label required")
		return
	}
	if len(label) > maxTagLabelLen {
		writeJSONError(w, http.StatusBadRequest, "label too long")
		return
	}
	tag, err := h.Tags.Rename(r.Context(), sess.UserID, id, label)
	if errors.Is(err, store.ErrTagDuplicate) {
		writeJSONError(w, http.StatusConflict, "another tag already owns that label")
		return
	}
	if err != nil {
		h.Logger.Error("rename tag", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "rename failed")
		return
	}
	if tag == nil {
		writeJSONError(w, http.StatusNotFound, "tag not found")
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

type mergeTagRequest struct {
	IntoTagID string `json:"into_tag_id"`
}

// Merge folds the path-id tag into into_tag_id, rewriting every link
// row. Both tags must belong to the caller; cross-tenant merges return
// 404. Idempotent — re-merging is a no-op once src is already 'merged'.
func (h *TagHandler) Merge(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	src := chi.URLParam(r, "id")
	var req mergeTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	dst := strings.TrimSpace(req.IntoTagID)
	if dst == "" {
		writeJSONError(w, http.StatusBadRequest, "into_tag_id required")
		return
	}
	if err := h.Tags.Merge(r.Context(), sess.UserID, src, dst); err != nil {
		// The store returns a generic "not found" error when either tag
		// is missing or wrong-tenant; surface as 404 either way.
		if strings.Contains(err.Error(), "not found") {
			writeJSONError(w, http.StatusNotFound, "tag not found")
			return
		}
		if strings.Contains(err.Error(), "itself") {
			writeJSONError(w, http.StatusBadRequest, "cannot merge into self")
			return
		}
		h.Logger.Error("merge tag", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "merge failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Archive handles DELETE /api/tags/:id — soft delete. History rows in
// daily_entry_tags survive (the tag still resolves for past-day reads).
func (h *TagHandler) Archive(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.Tags.Archive(r.Context(), sess.UserID, id); err != nil {
		h.Logger.Error("archive tag", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "archive failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validTagValence(v string) bool {
	switch v {
	case domain.TagValencePositive, domain.TagValenceNegative, domain.TagValenceNeutral:
		return true
	}
	return false
}
