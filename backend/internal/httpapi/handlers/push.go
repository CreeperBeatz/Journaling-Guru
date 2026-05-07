package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/httpapi/middleware"
	"github.com/cosmosthrace/journai/backend/internal/push"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// ReminderReplanner is the interface the handler uses to re-arm reminder
// jobs after a settings or subscription change. Implemented by
// *jobs.ReminderScheduler. Wrapped behind an interface so handler tests
// can pass a fake.
type ReminderReplanner interface {
	Replan(ctx context.Context, userID string) error
}

// PushHandler hosts /api/push/*. The endpoints are minimal:
// vapid-public-key (read), subscribe (POST/DELETE), test (POST). Worker-
// side concerns (encryption, fan-out, retries) live in internal/push +
// internal/jobs — this handler is just the persistence + Replan glue.
type PushHandler struct {
	Subs          *store.PushSubscriptionStore
	Users         *store.UserStore
	Reminders     *store.ReminderJobStore
	Replanner     ReminderReplanner
	Sender        push.Sender // nil-safe: nil → 503 on /test
	Logger        *slog.Logger
	VAPIDPublic   string
	AppOrigin     string // origin used for the test-push URL
}

// VAPIDKeyResponse carries the public key the browser feeds into
// PushManager.subscribe(applicationServerKey). The body is identical to
// the env var so the FE can pass it through verbatim — no transformation.
type VAPIDKeyResponse struct {
	PublicKey string `json:"public_key"`
}

// VAPIDKey returns the configured VAPID public key. Public endpoint
// (no auth) — the key isn't a secret, and a logged-out PWA still
// needs it to register a SW subscription on first use.
//
// Returns 503 when keys aren't set so the FE can render an "ask the
// admin" hint instead of failing silently.
func (h *PushHandler) VAPIDKey(w http.ResponseWriter, r *http.Request) {
	if h.VAPIDPublic == "" {
		writeJSONError(w, http.StatusServiceUnavailable, "push not configured")
		return
	}
	writeJSON(w, http.StatusOK, VAPIDKeyResponse{PublicKey: h.VAPIDPublic})
}

type subscribeRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// Subscribe binds (endpoint) → user. Idempotent on endpoint via the
// store's UPSERT. Triggers a Replan because a freshly-subscribed
// device should produce a future reminder_jobs row even if none was
// scheduled before (e.g. user disabled then re-enabled reminders).
func (h *PushHandler) Subscribe(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req subscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Endpoint == "" || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		writeJSONError(w, http.StatusBadRequest, "endpoint and keys required")
		return
	}
	// Sanity-cap endpoint length — push service URLs are ~200-400 chars.
	// Refuse anything wildly outside that range to keep the table tidy.
	if len(req.Endpoint) > 2048 {
		writeJSONError(w, http.StatusBadRequest, "endpoint too long")
		return
	}

	ua := userAgent(r)
	var uaPtr *string
	if ua != "" {
		s := ua
		if len(s) > 500 {
			s = s[:500]
		}
		uaPtr = &s
	}

	row, err := h.Subs.Upsert(r.Context(), sess.UserID, req.Endpoint, req.Keys.P256dh, req.Keys.Auth, uaPtr)
	if err != nil {
		h.Logger.Error("upsert push subscription", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "subscribe failed")
		return
	}

	// Re-arm the reminder slot. A user could have been in the
	// "no subscriptions, skipped, scheduled tomorrow" state; subscribing
	// today should bring forward the next reminder if it falls before
	// the existing tomorrow row.
	if h.Replanner != nil {
		if err := h.Replanner.Replan(r.Context(), sess.UserID); err != nil {
			h.Logger.Warn("replan after subscribe", "err", err, "user_id", sess.UserID)
		}
	}

	writeJSON(w, http.StatusOK, row)
}

type unsubscribeRequest struct {
	Endpoint string `json:"endpoint"`
}

// Unsubscribe drops one (user, endpoint) pair. Returns 200 even if the
// endpoint wasn't on file — the FE calls this defensively when the SW
// reports a subscription that the server didn't know about.
func (h *PushHandler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req unsubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Endpoint == "" {
		writeJSONError(w, http.StatusBadRequest, "endpoint required")
		return
	}
	if err := h.Subs.DeleteByEndpoint(r.Context(), sess.UserID, req.Endpoint); err != nil {
		h.Logger.Error("delete push subscription", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "unsubscribe failed")
		return
	}
	writeJSON(w, http.StatusOK, ackResponse{OK: true})
}

// SubscriptionsState is the FE's read endpoint: count + lightweight
// device list (user_agent + last_used_at). Endpoint URLs are kept off
// the response — the FE only ever needs the SW's currentSubscription
// for unsubscribe, never the server's mirror.
type SubscriptionsState struct {
	Count   int                            `json:"count"`
	Devices []SubscriptionsStateDeviceItem `json:"devices"`
}
type SubscriptionsStateDeviceItem struct {
	ID         string  `json:"id"`
	UserAgent  *string `json:"user_agent,omitempty"`
	LastUsedAt string  `json:"last_used_at"`
}

// State returns "is the current user subscribed, and from which UAs."
// Used by Settings → Notifications to show "subscribed on Chrome
// (last used 2h ago)" instead of just a generic toggle.
func (h *PushHandler) State(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	subs, err := h.Subs.ListByUser(r.Context(), sess.UserID)
	if err != nil {
		h.Logger.Error("list subscriptions", "err", err, "user_id", sess.UserID)
		writeJSONError(w, http.StatusInternalServerError, "state failed")
		return
	}
	out := SubscriptionsState{
		Count:   len(subs),
		Devices: make([]SubscriptionsStateDeviceItem, 0, len(subs)),
	}
	for _, s := range subs {
		out.Devices = append(out.Devices, SubscriptionsStateDeviceItem{
			ID:         s.ID,
			UserAgent:  s.UserAgent,
			LastUsedAt: s.LastUsedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// Test fires a single notification to every subscription the user has,
// right now. End-to-end smoke test for the subscribe flow without
// waiting for the next scheduled reminder.
//
// Bypasses reminder_jobs entirely — this is a debug helper, not part
// of the cadence.
func (h *PushHandler) Test(w http.ResponseWriter, r *http.Request) {
	sess := middleware.SessionFromCtx(r.Context())
	if sess == nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	if h.Sender == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "push not configured")
		return
	}

	subs, err := h.Subs.ListByUser(r.Context(), sess.UserID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "list subscriptions failed")
		return
	}
	if len(subs) == 0 {
		writeJSONError(w, http.StatusBadRequest, "no subscriptions to test")
		return
	}

	// Same payload shape the worker uses; SW handler renders both.
	payload, err := json.Marshal(map[string]string{
		"title": "JournAI test notification",
		"body":  "If you're seeing this, push is working.",
		"url":   strings.TrimRight(h.AppOrigin, "/") + "/today",
		"tag":   "test-" + sess.UserID,
	})
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "encode payload failed")
		return
	}

	delivered, gone, retryable := 0, 0, 0
	var lastErr string
	for _, sub := range subs {
		outcome, err := h.Sender.Send(r.Context(), push.Subscription{
			Endpoint: sub.Endpoint, P256dh: sub.P256dh, Auth: sub.Auth,
		}, payload)
		switch outcome {
		case push.OutcomeDelivered:
			delivered++
			h.Subs.MarkSuccess(r.Context(), sub.ID)
		case push.OutcomeGone:
			gone++
			_ = h.Subs.DeleteByID(r.Context(), sub.ID)
		case push.OutcomeRetryable:
			retryable++
			if err != nil {
				lastErr = err.Error()
			}
			_, _ = h.Subs.IncrementFailure(r.Context(), sub.ID)
		}
	}
	if delivered == 0 && retryable > 0 {
		// Surface the underlying error so the FE can show "VAPID key
		// mismatch" or "push service down" instead of silently noopping.
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"delivered": delivered,
			"gone":      gone,
			"retryable": retryable,
			"error":     lastErr,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"delivered": delivered,
		"gone":      gone,
		"retryable": retryable,
	})
}

