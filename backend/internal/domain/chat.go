package domain

import "time"

// Chat session phase ENUM. Drives both the UI gate (which controls show
// for "I'm done" affordance, ExtractionPending overlay, etc.) and the
// system-prompt phase block (different instructions per phase).
const (
	ChatPhaseGreeting    = "greeting"
	ChatPhaseExploring   = "exploring"
	ChatPhaseWrappingUp  = "wrapping_up"
	ChatPhaseFinalized   = "finalized"
	ChatPhaseAbandoned   = "abandoned"
)

// Chat session mode. Voice is reserved for Phase 6b; same row, different
// IO surface (WebRTC + OpenAI Realtime instead of SSE + OpenRouter).
const (
	ChatModeText  = "text"
	ChatModeVoice = "voice"
)

// Chat session scope. Daily is the original journal-companion chat; weekly
// is the reflection chat that lives behind the weekly wizard's step 2.
// Persona, tool set, and finalize behaviour all branch on this.
const (
	ChatScopeDaily  = "daily"
	ChatScopeWeekly = "weekly"
)

// Chat message role ENUM mirrors OpenAI chat-completions, plus
// system_event for server-injected breadcrumbs that the prompt builder
// can surface contextually but the UI styles distinctly.
const (
	ChatRoleUser        = "user"
	ChatRoleAssistant   = "assistant"
	ChatRoleTool        = "tool"
	ChatRoleSystemEvent = "system_event"
)

// Chat extraction job + session status ENUM. Lifecycle:
//   idle → pending (on finalize) → running (worker claims) →
//     completed | failed.
const (
	ChatExtractionIdle      = "idle"
	ChatExtractionPending   = "pending"
	ChatExtractionRunning   = "running"
	ChatExtractionCompleted = "completed"
	ChatExtractionFailed    = "failed"
)

// ChatSession is the public-facing shape of a row in `chat_sessions`.
// LocalDate is wire-formatted as YYYY-MM-DD; timestamps render as RFC3339
// via the default JSON marshaler. UserID is hidden from JSON because the
// session cookie already disambiguates ownership and we never want to
// leak someone else's id by accident.
type ChatSession struct {
	ID                  string     `json:"id"`
	UserID              string     `json:"-"`
	LocalDate           string     `json:"local_date"`
	// Scope is "daily" (one session per user per local_date) or "weekly"
	// (one session per user per week_start). For weekly rows LocalDate is
	// set equal to PeriodStart so the existing as-of lookups keep working.
	Scope               string     `json:"scope"`
	// PeriodStart is the week_start date for scope="weekly", NULL for daily.
	PeriodStart         *string    `json:"period_start,omitempty"`
	Mode                string     `json:"mode"`
	Phase               string     `json:"phase"`
	ChatModel           string     `json:"chat_model"`
	ExtractionModel     string     `json:"extraction_model"`
	OpenAISessionID     *string    `json:"openai_session_id,omitempty"`
	StartedAt           time.Time  `json:"started_at"`
	LastActivityAt      time.Time  `json:"last_activity_at"`
	EndedAt             *time.Time `json:"ended_at,omitempty"`
	FinalizedAt         *time.Time `json:"finalized_at,omitempty"`
	ExtractionStatus    string     `json:"extraction_status"`
	ExtractionError     *string    `json:"extraction_error,omitempty"`
	// CoveredQuestionIDs is the authoritative set written by the
	// post-turn classifier; the FE renders coverage chips from it
	// (initial value at page load, then live updates via the SSE
	// coverage_update event during streaming).
	CoveredQuestionIDs []string `json:"covered_question_ids"`
	// CoverageLastClassifiedSeq is the max chat_messages.seq the
	// classifier has already processed for this session. Used to skip
	// the LLM round-trip when there's no new user content since the
	// previous classification. JSON-hidden — internal scheduling state.
	CoverageLastClassifiedSeq int       `json:"-"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

// ChatMessage is one row in the transcript. ToolName/ToolArgs are set on
// assistant rows that emitted a tool call; ToolResult is set on tool
// rows that carry the server's response. Token counts are 0 except on
// assistant rows we sent through the LLM.
//
// Content is the rendered text for user/assistant rows. For tool /
// system_event rows it can be a human-readable label ("user_dismissed_
// crisis", "idle_auto_finalize"); the structured payload lives in the
// tool_args / tool_result columns.
type ChatMessage struct {
	ID         string         `json:"id"`
	SessionID  string         `json:"session_id"`
	Seq        int            `json:"seq"`
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolName   *string        `json:"tool_name,omitempty"`
	ToolArgs   map[string]any `json:"tool_args,omitempty"`
	ToolResult map[string]any `json:"tool_result,omitempty"`
	TokenIn    int            `json:"token_in"`
	TokenOut   int            `json:"token_out"`
	CreatedAt  time.Time      `json:"created_at"`
}

// ChatExtractionJob is the queue row for the async extraction worker.
// Mirrors SummaryJob's shape — one River InsertOpts and the same
// dispatcher claim semantics.
type ChatExtractionJob struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id"`
	UserID     string     `json:"-"`
	FireAt     time.Time  `json:"fire_at"`
	FiredAt    *time.Time `json:"fired_at,omitempty"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	LastError  *string    `json:"last_error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// IsValidChatPhase reports whether s is one of the five enum values. The
// CHECK constraint enforces this at the DB layer; this helper exists so
// the AdvancePhase store method can return a typed error before the SQL
// round-trip.
func IsValidChatPhase(s string) bool {
	switch s {
	case ChatPhaseGreeting, ChatPhaseExploring, ChatPhaseWrappingUp,
		ChatPhaseFinalized, ChatPhaseAbandoned:
		return true
	}
	return false
}

// LegalChatPhaseTransition reports whether `from → to` is a valid phase
// transition. The phase is informational — it drives UI hints (composer
// placeholder, wrap-up affordance) but does NOT lock the session. A
// chat for a given local_date stays open all day; finalize only triggers
// an extraction. After extraction completes the worker rolls the phase
// back to exploring so the user can keep talking.
//
// Allowed transitions:
//   greeting    → exploring | wrapping_up | abandoned
//   exploring   → wrapping_up | finalized
//   wrapping_up → exploring | finalized | abandoned (resume by typing)
//   finalized   → exploring | wrapping_up          (resume after extraction)
//   abandoned   → exploring                        (rare — sweeper bailout)
//
// finalized is reachable for legacy reasons (the worker briefly stamps
// it inside the extraction tx); the worker then immediately advances
// back to exploring. abandoned is reserved for sweeper failures: the
// extraction worker marks an opener-only transcript abandoned from
// either greeting (sweeper skipped the wrapping_up advance — nothing to
// land) or wrapping_up (sessions swept before that skip existed).
func LegalChatPhaseTransition(from, to string) bool {
	if !IsValidChatPhase(from) || !IsValidChatPhase(to) {
		return false
	}
	switch from {
	case ChatPhaseGreeting:
		return to == ChatPhaseExploring || to == ChatPhaseWrappingUp ||
			to == ChatPhaseAbandoned
	case ChatPhaseExploring:
		return to == ChatPhaseWrappingUp || to == ChatPhaseFinalized
	case ChatPhaseWrappingUp:
		return to == ChatPhaseExploring || to == ChatPhaseFinalized ||
			to == ChatPhaseAbandoned
	case ChatPhaseFinalized:
		return to == ChatPhaseExploring || to == ChatPhaseWrappingUp
	case ChatPhaseAbandoned:
		return to == ChatPhaseExploring
	}
	return false
}
