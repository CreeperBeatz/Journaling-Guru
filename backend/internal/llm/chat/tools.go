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
	ToolProposeWrapUp       = "propose_wrap_up"
	ToolProposeGoal         = "propose_goal"
	ToolProposeExtendGoal   = "propose_extend_goal"
	ToolProposeCompleteGoal = "propose_complete_goal"
	ToolProposeIntention    = "propose_intention"
)

// AssistantTools is the list of tool defs sent to the LLM on every
// streaming turn of a DAILY chat. Order is stable for prompt-caching.
//
// propose_wrap_up flips the session phase to wrapping_up so the UI can
// surface the "I'm done" affordance.
var AssistantTools = []llm.ToolDef{
	{
		Name: ToolProposeWrapUp,
		Description: "Signal that the user is winding the session down (said \"that's it\", " +
			"\"I should sleep\", or has clearly stopped engaging). The UI will surface a wrap-up " +
			"affordance; the user clicks it to end. Do NOT announce the call. " +
			"GATE: only call this once the user has substantively addressed drained, charged, " +
			"and grateful AND has given a yes/no/explicit-refusal for every active goal in the " +
			"session context. If any of those is still open, ask about it instead — do not call " +
			"this tool yet.",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	},
}

// WeeklyAssistantTools is the tool list for weekly reflection chats.
// The three proposal tools surface inline confirmation cards in the
// frontend — the tool calls themselves DO NOT write to the database.
// The user accepts/edits/declines from the rendered card, and the
// frontend then hits the existing /api/goals endpoints to persist.
//
// propose_wrap_up is gated specifically for the weekly scope: every
// "ending this week" goal listed in the system context must have a
// prior propose_extend_goal or propose_complete_goal call earlier in
// the transcript.
var WeeklyAssistantTools = []llm.ToolDef{
	{
		Name: ToolProposeGoal,
		Description: "Propose ONE small, achievable goal for next week. Only call once the user " +
			"has spoken — in their own words — to (1) why this matters to them, (2) what they " +
			"think happens if they follow it, and (3) what happens if they don't. The why_matters, " +
			"if_followed, and if_not_followed fields go VERBATIM from the user's words. Don't " +
			"invent them. You MUST emit at least one short plain-text sentence BEFORE the tool " +
			"call. The UI surfaces an editable confirmation card; the user accepts or declines.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title": map[string]any{
					"type":        "string",
					"description": "Short imperative goal title (1..120 chars). E.g. 'Walk 20 minutes before lunch.'",
					"minLength":   1,
					"maxLength":   120,
				},
				"check_in_question": map[string]any{
					"type":        "string",
					"description": "The yes/no question we'll ask each day. E.g. 'Did you walk for 20+ minutes today?'",
					"minLength":   1,
					"maxLength":   160,
				},
				"why_matters": map[string]any{
					"type":        "string",
					"description": "Verbatim from user: why this matters to them. Keep tight (≤300 chars) — quote the load-bearing phrase, not the whole turn.",
					"minLength":   1,
					"maxLength":   300,
				},
				"if_followed": map[string]any{
					"type":        "string",
					"description": "Verbatim from user: what they think happens if they follow it. ≤300 chars.",
					"minLength":   1,
					"maxLength":   300,
				},
				"if_not_followed": map[string]any{
					"type":        "string",
					"description": "Verbatim from user: what happens if they don't. ≤300 chars.",
					"minLength":   1,
					"maxLength":   300,
				},
				"duration_weeks": map[string]any{
					"type":        "integer",
					"description": "How many weeks to run this goal (1..12). Default 1 for tiny goals.",
					"minimum":     1,
					"maximum":     12,
				},
			},
			"required":             []string{"title", "check_in_question", "why_matters", "if_followed", "if_not_followed", "duration_weeks"},
			"additionalProperties": false,
		},
	},
	{
		Name: ToolProposeExtendGoal,
		Description: "Propose extending an ENDING goal (one whose end_date is on or before this " +
			"week's reflection date, listed in the 'Ending this week' section of context). Only " +
			"call after the user has indicated they want to continue the goal. The UI shows an " +
			"editable card; the user confirms or declines.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "UUID of the goal from the 'Ending this week' section.",
				},
				"weeks": map[string]any{
					"type":        "integer",
					"description": "How many additional weeks to extend (1..12).",
					"minimum":     1,
					"maximum":     12,
				},
			},
			"required":             []string{"goal_id", "weeks"},
			"additionalProperties": false,
		},
	},
	{
		Name: ToolProposeCompleteGoal,
		Description: "Propose marking an ENDING goal as complete. Outcome is 'kept' (user kept " +
			"the habit most/all days), 'dropped' (user stopped trying), or 'inconclusive' (mixed " +
			"or unclear). reason carries the user's own words on how it went. Only call once the " +
			"user has clearly settled on a verdict.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{
					"type":        "string",
					"description": "UUID of the goal from the 'Ending this week' section.",
				},
				"outcome": map[string]any{
					"type":        "string",
					"description": "How the goal went.",
					"enum":        []string{"kept", "dropped", "inconclusive"},
				},
				"reason": map[string]any{
					"type":        "string",
					"description": "Verbatim from user: how it actually went, in their words.",
					"minLength":   1,
					"maxLength":   600,
				},
			},
			"required":             []string{"goal_id", "outcome", "reason"},
			"additionalProperties": false,
		},
	},
	{
		Name: ToolProposeWrapUp,
		Description: "Signal that the weekly reflection is winding down. " +
			"GATE: only call this once every goal in the 'Ending this week' section has " +
			"received a propose_extend_goal OR propose_complete_goal tool call earlier in the " +
			"transcript. If any ending goal is still open, ask about that goal instead. " +
			"You MUST emit at least one short plain-text sentence BEFORE the tool call.",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	},
}

// MonthlyAssistantTools is the tool list for COMBINED weekly+monthly
// reflection sessions (chat_sessions.month_period_start set). It is the
// weekly list plus propose_intention, with propose_wrap_up's gate
// extended to also require an intention. A separate slice — never mutate
// WeeklyAssistantTools — so plain weekly sessions keep a byte-identical
// tool list for prompt caching.
var MonthlyAssistantTools = []llm.ToolDef{
	WeeklyAssistantTools[0], // propose_goal
	WeeklyAssistantTools[1], // propose_extend_goal
	WeeklyAssistantTools[2], // propose_complete_goal
	{
		Name: ToolProposeIntention,
		Description: "Propose the user's ONE intention/theme for next month. Broader than a " +
			"weekly tiny goal — a direction, not a habit (e.g. 'Protect my mornings', 'Say yes " +
			"to invitations'). Only call once the user has named the intention in their own " +
			"words AND spoken to why it matters. Both fields go VERBATIM from the user's " +
			"words — don't invent or polish them. You MUST emit at least one short plain-text " +
			"sentence BEFORE the tool call. The UI surfaces an editable confirmation card; the " +
			"user accepts, edits, or declines. Call at most once unless the user changes their " +
			"mind after declining.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"intention": map[string]any{
					"type":        "string",
					"description": "The intention, near-verbatim from the user (1..200 chars). Theme-shaped, not habit-shaped.",
					"minLength":   1,
					"maxLength":   200,
				},
				"why_matters": map[string]any{
					"type":        "string",
					"description": "Verbatim from user: why this direction matters to them. ≤300 chars — quote the load-bearing phrase.",
					"minLength":   1,
					"maxLength":   300,
				},
			},
			"required":             []string{"intention", "why_matters"},
			"additionalProperties": false,
		},
	},
	{
		Name: ToolProposeWrapUp,
		Description: "Signal that the combined weekly+monthly reflection is winding down. " +
			"GATE: only call this once (1) every goal in the 'Ending this week' section has " +
			"received a propose_extend_goal OR propose_complete_goal tool call earlier in the " +
			"transcript, AND (2) a propose_intention call exists earlier in the transcript. " +
			"If an ending goal is still open, ask about that goal; if no intention has been " +
			"shaped yet, steer there instead. " +
			"You MUST emit at least one short plain-text sentence BEFORE the tool call.",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	},
}
