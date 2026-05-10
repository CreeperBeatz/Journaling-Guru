// Package realtime is a narrow client for OpenAI's Realtime API. The
// only operation we need server-side is minting an ephemeral
// client_secret — the browser holds that secret and connects to OpenAI
// directly via WebRTC, so audio never traverses the Go backend. Voice
// transcripts come back to us as POSTed chat_messages rows from the
// browser data channel listener.
package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrNoAPIKey is returned when MintEphemeralSecret is called without an
// OPENAI_API_KEY configured. The HTTP handler maps this to 503 so a dev
// environment without the key still serves text chat.
var ErrNoAPIKey = errors.New("openai realtime: API key not configured")

// Client is a thin OpenAI Realtime wrapper. The HTTP client is shared so
// keep-alive amortizes across mints; constructed once at server start.
type Client struct {
	APIKey   string
	Model    string
	BaseURL  string
	HTTP     *http.Client
}

// New builds a client with sensible defaults. Pass empty BaseURL for
// the production OpenAI endpoint.
func New(apiKey, model string) *Client {
	return &Client{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://api.openai.com/v1",
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// MintRequest is the per-call shape. Instructions is the composed system
// prompt (BuildSystemPrompt output, with a voice-tone tweak).
type MintRequest struct {
	// Model overrides the client default when non-empty.
	Model        string
	Instructions string
	// Voice picks the assistant voice. Realtime GA defaults to "marin";
	// we expose it so a future per-user knob can flow through.
	Voice string
	// SafetyIdentifier binds an OpenAI-Safety-Identifier to the
	// resulting ephemeral token. Pass a stable hashed user id so abuse
	// signals from this user can be correlated server-side without the
	// browser ever sending the raw id over the data channel.
	SafetyIdentifier string
}

// MintResponse is what we return to the browser. Value is the ephemeral
// secret (starts with "ek_..."), valid for ~60s and single-use to open
// a Realtime session. ExpiresAt is unix seconds.
type MintResponse struct {
	Value     string `json:"value"`
	ExpiresAt int64  `json:"expires_at"`
	// SessionID is the OpenAI-side Realtime session id, returned on
	// the parent session object. Persisted in chat_sessions.openai_session_id
	// so we can correlate logs/billing later.
	SessionID string `json:"session_id"`
	Model     string `json:"model"`
}

// MintEphemeralSecret POSTs to /realtime/client_secrets and returns the
// short-lived bearer the browser uses for its WebRTC handshake.
//
// The OpenAI request schema (Dec 2025): the body wraps a `session`
// object with `type: "realtime"` plus per-session config. The response
// body returns `{ value, expires_at, session: { id, ... } }`.
func (c *Client) MintEphemeralSecret(ctx context.Context, req MintRequest) (*MintResponse, error) {
	if c.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	model := req.Model
	if model == "" {
		model = c.Model
	}
	voice := req.Voice
	if voice == "" {
		voice = "marin"
	}

	// Current GA shape (2026-05): audio.input config takes both a
	// transcription model and turn_detection. Semantic VAD is the
	// recommended default — it cuts the model in only after the user
	// has actually finished a thought, instead of every 300ms of
	// silence.
	body := map[string]any{
		"session": map[string]any{
			"type":         "realtime",
			"model":        model,
			"instructions": req.Instructions,
			"audio": map[string]any{
				"input": map[string]any{
					"transcription": map[string]any{
						"model": "gpt-4o-mini-transcribe",
					},
					"turn_detection": map[string]any{
						"type": "semantic_vad",
					},
				},
				"output": map[string]any{
					"voice": voice,
				},
			},
		},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/realtime/client_secrets", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if req.SafetyIdentifier != "" {
		// Bound to the resulting ephemeral token; the browser doesn't
		// need to forward this header on its WebRTC handshake.
		httpReq.Header.Set("OpenAI-Safety-Identifier", req.SafetyIdentifier)
	}
	// No OpenAI-Beta header — the GA Realtime API rejects beta-tagged
	// client_secrets with api_version_mismatch.

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openai realtime: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openai realtime %d: %s", resp.StatusCode, truncate(string(raw), 500))
	}

	// GA response shape (Dec 2025+): the response IS the session object,
	// with the ephemeral secret materialized as top-level `value` +
	// `expires_at`. Older SDK descriptions also document a nested
	// `client_secret: { value, expires_at }` shape — accept both for
	// forward/backward compatibility.
	var parsed struct {
		Value        string `json:"value"`
		ExpiresAt    int64  `json:"expires_at"`
		ID           string `json:"id"`
		Model        string `json:"model"`
		ClientSecret *struct {
			Value     string `json:"value"`
			ExpiresAt int64  `json:"expires_at"`
		} `json:"client_secret"`
		Session *struct {
			ID    string `json:"id"`
			Model string `json:"model"`
		} `json:"session"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode: %w (body: %s)", err, truncate(string(raw), 200))
	}
	value := parsed.Value
	expiresAt := parsed.ExpiresAt
	if value == "" && parsed.ClientSecret != nil {
		value = parsed.ClientSecret.Value
		if expiresAt == 0 {
			expiresAt = parsed.ClientSecret.ExpiresAt
		}
	}
	sessionID := parsed.ID
	respModel := parsed.Model
	if parsed.Session != nil {
		if sessionID == "" {
			sessionID = parsed.Session.ID
		}
		if respModel == "" {
			respModel = parsed.Session.Model
		}
	}
	if value == "" {
		return nil, fmt.Errorf("openai realtime: empty client_secret value (body: %s)", truncate(string(raw), 200))
	}
	if respModel == "" {
		respModel = model
	}
	return &MintResponse{
		Value:     value,
		ExpiresAt: expiresAt,
		SessionID: sessionID,
		Model:     respModel,
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
