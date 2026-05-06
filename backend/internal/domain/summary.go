package domain

import "time"

// SummaryPeriod is the calendar bucket a Summary covers.
type SummaryPeriod string

const (
	PeriodDay   SummaryPeriod = "day"
	PeriodWeek  SummaryPeriod = "week"
	PeriodMonth SummaryPeriod = "month"
	PeriodYear  SummaryPeriod = "year"
)

// IsValidPeriod reports whether s is one of the four enum values. The CHECK
// constraint enforces this at the DB layer; this helper exists so handlers
// can return 400 before the SQL round-trip.
func IsValidPeriod(s string) bool {
	switch SummaryPeriod(s) {
	case PeriodDay, PeriodWeek, PeriodMonth, PeriodYear:
		return true
	}
	return false
}

// SummaryMetadata is the structured stats payload stored in summaries.metadata.
//
// For daily summaries the LLM produces these fields directly (see
// llm/prompts/daily.tmpl). For weekly/monthly/yearly the worker computes
// them by aggregating constituent daily metadatas — we don't ask the LLM
// to do arithmetic over numeric fields it can't see.
type SummaryMetadata struct {
	Emotions   []string `json:"emotions,omitempty"`
	MoodScore  *float64 `json:"mood_score,omitempty"` // 1..10
	MoodLabel  string   `json:"mood_label,omitempty"` // "positive" | "neutral" | "negative"
	Topics     []string `json:"topics,omitempty"`
	EntryCount int      `json:"entry_count,omitempty"`
}

// Summary is the public-facing shape of a row in `summaries`. PeriodStart /
// PeriodEnd are wire-formatted as YYYY-MM-DD; the store layer converts to
// time.Time on the way in.
type Summary struct {
	ID               string          `json:"id"`
	UserID           string          `json:"-"`
	PeriodType       string          `json:"period_type"`
	PeriodStart      string          `json:"period_start"`
	PeriodEnd        string          `json:"period_end"`
	Body             string          `json:"body"`
	Metadata         SummaryMetadata `json:"metadata"`
	Model            string          `json:"model"`
	PromptTokens     int             `json:"prompt_tokens"`
	CompletionTokens int             `json:"completion_tokens"`
	GeneratedAt      time.Time       `json:"generated_at"`
}

// SummaryJob is the scheduler-queue row. Status lifecycle is documented in
// the migration; only the worker and the dispatcher write to it.
type SummaryJob struct {
	ID          string     `json:"id"`
	UserID      string     `json:"-"`
	PeriodType  string     `json:"period_type"`
	PeriodStart string     `json:"period_start"`
	FireAt      time.Time  `json:"fire_at"`
	FiredAt     *time.Time `json:"fired_at,omitempty"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	LastError   *string    `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}
