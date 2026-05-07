// Package chat hosts the chat-mode helpers: system-prompt builder,
// extraction step, safety filter, and tool definitions. All chat-mode
// behavior that's independent of HTTP transport lives here so the
// handler stays thin.
package chat

import "github.com/cosmosthrace/journai/backend/internal/llm"

// Tool names — used both in the prompt definitions and in the SSE
// handler's frame routing. Coverage tracking is no longer a tool;
// a post-turn classifier (see chat.Classify) writes the authoritative
// covered set after each assistant reply.
const (
	ToolProposeWrapUp = "propose_wrap_up"
)

// AssistantTools is the list of tool defs sent to the LLM on every
// streaming turn. Order is stable for prompt-caching.
//
// propose_wrap_up flips the session phase to wrapping_up so the UI can
// surface the "I'm done" affordance.
var AssistantTools = []llm.ToolDef{
	{
		Name: ToolProposeWrapUp,
		Description: "Signal that the user is winding the session down (said \"that's it\", " +
			"\"I should sleep\", or has clearly stopped engaging). The UI will surface a wrap-up " +
			"affordance; the user clicks it to end. Do NOT announce the call.",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	},
}
