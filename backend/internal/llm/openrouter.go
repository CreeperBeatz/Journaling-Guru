// Package llm wraps the OpenRouter chat-completions API. v2 uses
// OpenRouter so the model is a config knob, not a vendor lock-in — voice
// stays on OpenAI Realtime (different surface), summaries can route
// through any chat-completions-compatible model.
package llm

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

// OpenRouter is a thin chat-completions client. No streaming, no tool
// use, no vision — summaries are one-shot text/JSON.
type OpenRouter struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
	Referer string // HTTP-Referer header — OpenRouter uses it for app stats.
	Title   string // X-Title header — visible in OpenRouter dashboard.
}

// NewOpenRouter builds the client. Pass empty BaseURL for the default
// (api.openrouter.ai). If HTTP is nil we install a 60-second-timeout
// client — generous because Claude can take 30+ seconds on a yearly
// summary.
func NewOpenRouter(apiKey, model, referer, title string) *OpenRouter {
	return &OpenRouter{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://openrouter.ai/api/v1",
		HTTP:    &http.Client{Timeout: 90 * time.Second},
		Referer: referer,
		Title:   title,
	}
}

// Message is one chat turn. Role is "system" | "user" | "assistant".
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is the subset of the OpenAI-compatible body we use.
// JSONMode toggles `response_format: {type: "json_object"}` — most chat
// models honor it; the Anthropic stack ignores it but still produces
// valid JSON when the prompt instructs it to.
type CompletionRequest struct {
	System    string
	User      string
	MaxTokens int
	JSONMode  bool
}

// CompletionResponse is the parsed answer. Tokens are 0 when the upstream
// omits usage (rare for OpenRouter).
type CompletionResponse struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	Model            string
}

// ErrNoAPIKey is returned when the client is invoked without an
// OPENROUTER_API_KEY configured. Surfaced by the worker as a transient
// error so the dispatcher will retry once the operator wires the key.
var ErrNoAPIKey = errors.New("openrouter: API key not configured")

// Complete sends a chat-completions request and returns the assistant's
// reply. Errors are returned verbatim — caller decides retry policy.
func (c *OpenRouter) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if c.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	body := map[string]any{
		"model": c.Model,
		"messages": []Message{
			{Role: "system", Content: req.System},
			{Role: "user", Content: req.User},
		},
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.JSONMode {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if c.Referer != "" {
		httpReq.Header.Set("HTTP-Referer", c.Referer)
	}
	if c.Title != "" {
		httpReq.Header.Set("X-Title", c.Title)
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("openrouter %d: %s", resp.StatusCode, truncate(string(raw), 500))
	}

	var parsed struct {
		Model   string `json:"model"`
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(raw), 200))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("openrouter returned no choices")
	}
	return &CompletionResponse{
		Content:          parsed.Choices[0].Message.Content,
		PromptTokens:     parsed.Usage.PromptTokens,
		CompletionTokens: parsed.Usage.CompletionTokens,
		Model:            parsed.Model,
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
