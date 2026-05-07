// Package llm wraps the OpenRouter chat-completions API. v2 uses
// OpenRouter so the model is a config knob, not a vendor lock-in — voice
// stays on OpenAI Realtime (different surface), summaries can route
// through any chat-completions-compatible model.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenRouter is a thin chat-completions client. Supports one-shot
// (Complete) and streamed (CompleteStream) calls. No vision, no native
// tool use — chat-mode tools are encoded in the prompt and parsed out
// of assistant content.
type OpenRouter struct {
	APIKey  string
	Model   string
	BaseURL string
	HTTP    *http.Client
	Referer string // HTTP-Referer header — OpenRouter uses it for app stats.
	Title   string // X-Title header — visible in OpenRouter dashboard.
}

// NewOpenRouter builds the client. Pass empty BaseURL for the default
// (api.openrouter.ai). HTTP gets a 90-second-timeout client by default —
// generous because Claude can take 30+ seconds on a yearly summary;
// streaming calls override the per-request timeout via context.
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

// Message is one chat turn. Role is "system" | "user" | "assistant" | "tool".
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompletionRequest is the subset of the OpenAI-compatible body we use
// for one-shot calls. Model overrides c.Model when non-empty (chat
// extraction uses Haiku-tier; summaries use Sonnet-tier; both share the
// client).
type CompletionRequest struct {
	Model     string // optional per-call override; empty = c.Model
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

// resolveModel returns the per-call override or falls back to the
// client default. Empty string means "use whatever's configured".
func (c *OpenRouter) resolveModel(override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return c.Model
}

// Complete sends a chat-completions request and returns the assistant's
// reply. Errors are returned verbatim — caller decides retry policy.
func (c *OpenRouter) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if c.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	body := map[string]any{
		"model": c.resolveModel(req.Model),
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
	c.applyAuthHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

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

// applyAuthHeaders sets the OpenRouter-required headers shared by both
// blocking and streaming calls.
func (c *OpenRouter) applyAuthHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	if c.Referer != "" {
		req.Header.Set("HTTP-Referer", c.Referer)
	}
	if c.Title != "" {
		req.Header.Set("X-Title", c.Title)
	}
}

// ----------------------- Streaming -----------------------

// StreamChunk is one delta from CompleteStream. The channel emits
// chunks in arrival order; exactly one terminal chunk (Done or Err)
// closes the stream.
//
// Mutually exclusive: at most one of Delta, ToolCall, Done, Err is set
// per chunk. Empty Delta="" with all others nil is a keepalive (the
// caller should not flush a token frame for it).
type StreamChunk struct {
	Delta    string         // text token(s); send to client as-is
	ToolCall *StreamToolCall // model emitted a complete tool call
	Done     *StreamDone     // terminal success
	Err      error           // terminal failure (channel closes after)
}

// StreamToolCall is a complete tool call assembled from streamed deltas.
// OpenRouter's stream emits tool args incrementally; we accumulate
// them server-side and emit a single chunk per fully-assembled tool
// call so the handler doesn't need streaming-aware tool plumbing.
type StreamToolCall struct {
	Name string
	Args map[string]any
}

// StreamDone carries the terminal usage stats. Sent once after the
// upstream `[DONE]` sentinel, just before the channel closes.
type StreamDone struct {
	Model            string
	PromptTokens     int
	CompletionTokens int
	FinishReason     string
}

// StreamRequest is the input to CompleteStream. Mirrors CompletionRequest
// plus tool defs and prior turns. Messages should NOT include the system
// row — set System separately.
type StreamRequest struct {
	Model           string // optional per-call override
	System          string
	Messages        []Message
	Tools           []ToolDef
	MaxTokens       int
	Temperature     float64 // 0 = unset (let upstream default)
	SystemCacheable bool    // emit Anthropic prompt-caching marker
}

// ToolDef is the JSON-schema-shaped description of a tool the model can
// call. Mirrors OpenAI's `tools` payload.
type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"` // JSON schema
}

// CompleteStream streams a chat-completions response. Returns a buffered
// channel (16) of chunks; caller MUST drain or context-cancel to release
// the underlying HTTP connection.
//
// Channel semantics:
//   - Closed after a terminal Done or Err chunk.
//   - On context cancel, reader exits, channel closes — no Done/Err
//     emitted (caller already knows it cancelled).
//   - Delta chunks may be batched ("Hi", " there") or single-token; the
//     UI's job is to append.
//   - SystemCacheable=true wraps the system message with Anthropic
//     prompt-cache markers (no-op on non-Anthropic models — OpenRouter
//     forwards the field unchanged).
func (c *OpenRouter) CompleteStream(
	ctx context.Context, req StreamRequest,
) (<-chan StreamChunk, error) {
	if c.APIKey == "" {
		return nil, ErrNoAPIKey
	}
	body, err := buildStreamBody(c.resolveModel(req.Model), req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.applyAuthHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	// Use a transport without the default 90s timeout — streaming responses
	// are long-lived. Context controls cancellation.
	streamingClient := &http.Client{Timeout: 0, Transport: c.HTTP.Transport}
	resp, err := streamingClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter request: %w", err)
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("openrouter %d: %s", resp.StatusCode, truncate(string(raw), 500))
	}

	out := make(chan StreamChunk, 16)
	go decodeStream(ctx, resp.Body, out)
	return out, nil
}

// buildStreamBody marshals the streaming request body. cache_control is
// applied as Anthropic-flavor `ephemeral` markers when SystemCacheable
// is set — OpenRouter passes the field through unchanged for Anthropic
// providers and ignores it for others.
func buildStreamBody(model string, req StreamRequest) ([]byte, error) {
	systemContent := req.System
	var systemBlock any
	if req.SystemCacheable && systemContent != "" {
		systemBlock = []map[string]any{
			{
				"type":          "text",
				"text":          systemContent,
				"cache_control": map[string]string{"type": "ephemeral"},
			},
		}
	} else if systemContent != "" {
		systemBlock = systemContent
	}

	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if systemBlock != nil {
		messages = append(messages, map[string]any{
			"role":    "system",
			"content": systemBlock,
		})
	}
	for _, m := range req.Messages {
		messages = append(messages, map[string]any{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   true,
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		body["tools"] = tools
		body["tool_choice"] = "auto"
	}
	return json.Marshal(body)
}

// decodeStream reads SSE frames from the upstream body, parses each
// `data: {...}` line, and emits StreamChunk values to `out`. Closes the
// channel and the body on terminal frame, error, or context cancel.
//
// Tool calls are accumulated across deltas (OpenAI's stream emits
// `tool_calls[i].function.arguments` as a streamed string). When a
// finish_reason of `tool_calls` arrives, the assembled call is flushed
// as a single ToolCall chunk. Multi-tool-call models emit one entry per
// index — we emit one ToolCall per index seen.
func decodeStream(ctx context.Context, body io.ReadCloser, out chan<- StreamChunk) {
	defer body.Close()
	defer close(out)

	type pendingToolCall struct {
		Name    string
		ArgsRaw strings.Builder
	}

	scanner := bufio.NewScanner(body)
	// Default scanner buffer is 64KB; OpenRouter's per-event JSON is
	// well under that, but tool args can grow — bump the cap.
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	pending := map[int]*pendingToolCall{}
	var (
		modelName    string
		promptTok    int
		completionTok int
		finishReason string
	)

	flushChunk := func(ch StreamChunk) bool {
		select {
		case <-ctx.Done():
			return false
		case out <- ch:
			return true
		}
	}

	emitErr := func(err error) {
		_ = flushChunk(StreamChunk{Err: err})
	}

	for scanner.Scan() {
		// Honor cancellation between frames.
		if ctx.Err() != nil {
			return
		}
		line := scanner.Text()
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var frame struct {
			Model   string `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(payload), &frame); err != nil {
			// Tolerate non-JSON keepalive lines that some proxies inject;
			// only bubble decode errors that look like real frames.
			if strings.HasPrefix(payload, "{") {
				emitErr(fmt.Errorf("decode stream frame: %w", err))
				return
			}
			continue
		}
		if frame.Model != "" {
			modelName = frame.Model
		}
		if frame.Usage.PromptTokens > 0 {
			promptTok = frame.Usage.PromptTokens
		}
		if frame.Usage.CompletionTokens > 0 {
			completionTok = frame.Usage.CompletionTokens
		}

		for _, ch := range frame.Choices {
			if ch.Delta.Content != "" {
				if !flushChunk(StreamChunk{Delta: ch.Delta.Content}) {
					return
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				p, ok := pending[tc.Index]
				if !ok {
					p = &pendingToolCall{}
					pending[tc.Index] = p
				}
				if tc.Function.Name != "" {
					p.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					p.ArgsRaw.WriteString(tc.Function.Arguments)
				}
			}
			if ch.FinishReason != "" {
				finishReason = ch.FinishReason
			}
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		emitErr(fmt.Errorf("scan stream: %w", err))
		return
	}

	// Flush any assembled tool calls before the Done frame.
	for _, p := range pending {
		if p.Name == "" {
			continue
		}
		args := map[string]any{}
		if raw := strings.TrimSpace(p.ArgsRaw.String()); raw != "" {
			if err := json.Unmarshal([]byte(raw), &args); err != nil {
				// Don't kill the stream over a malformed tool call —
				// emit it with empty args + the raw under a key so the
				// caller can decide whether to fail or ignore.
				args = map[string]any{"_raw": raw, "_parse_error": err.Error()}
			}
		}
		if !flushChunk(StreamChunk{ToolCall: &StreamToolCall{Name: p.Name, Args: args}}) {
			return
		}
	}

	_ = flushChunk(StreamChunk{Done: &StreamDone{
		Model:            modelName,
		PromptTokens:     promptTok,
		CompletionTokens: completionTok,
		FinishReason:     finishReason,
	}})
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
