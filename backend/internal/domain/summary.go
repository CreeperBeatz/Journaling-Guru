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
//
// Weekly synthesis fields are populated by the weekly worker from a
// JSON-mode LLM call and surface inside the reflection wizard's Card 1
// (and the Trends-tab letter). The headline lives in Summary.Body so
// the dashboard Zone 1 keeps working unchanged.
//
// Charged / Drained / Grateful / Insights are the four paragraphs of
// the structured letter. ClosingQuestion is the question rendered as a
// pull-quote below them. Letter is the legacy single-string field kept
// around for already-stored rows — new synthesis writes the structured
// fields and leaves Letter empty.
type SummaryMetadata struct {
	Emotions        []string       `json:"emotions,omitempty"`
	MoodScore       *float64       `json:"mood_score,omitempty"` // 1..10
	MoodLabel       string         `json:"mood_label,omitempty"` // "positive" | "neutral" | "negative"
	Topics          []string       `json:"topics,omitempty"`
	EntryCount      int            `json:"entry_count,omitempty"`
	Letter          string         `json:"letter,omitempty"` // legacy free-form letter (pre-structured rows)
	Charged         string         `json:"charged,omitempty"`
	Drained         string         `json:"drained,omitempty"`
	Grateful        string         `json:"grateful,omitempty"`
	Insights        string         `json:"insights,omitempty"`
	Themes          []SummaryTheme `json:"themes,omitempty"`
	ClosingQuestion string         `json:"closing_question,omitempty"`

	// Monthly synthesis paragraphs (period_type='month' rows only). The
	// monthly letter looks back rather than auditing days: Arc is the
	// month's story told through its weeks, Recurring the threads that
	// kept showing up across weekly letters, GoalsRetro what the goal
	// ledger says about what actually matters. ClosingQuestion doubles as
	// the direction question on monthly rows.
	Arc        string `json:"arc,omitempty"`
	Recurring  string `json:"recurring,omitempty"`
	GoalsRetro string `json:"goals_retro,omitempty"`
}

// HasLetterSynthesis reports whether m has any letter-shaped content —
// either the legacy Letter blob or one of the four structured paragraphs.
// Used by handlers to decide whether to surface "arriving soon" + a
// backfill, and by the frontend's render gates.
func (m SummaryMetadata) HasLetterSynthesis() bool {
	return m.Letter != "" ||
		m.Charged != "" ||
		m.Drained != "" ||
		m.Grateful != "" ||
		m.Insights != ""
}

// HasMonthlySynthesis is the monthly-letter analogue of
// HasLetterSynthesis. Kept separate on purpose: weekly code paths gate on
// HasLetterSynthesis and all summary lookups are period_type-scoped.
func (m SummaryMetadata) HasMonthlySynthesis() bool {
	return m.Arc != "" ||
		m.Recurring != "" ||
		m.GoalsRetro != ""
}

// SummaryTheme is one ad-hoc grouping of related tags the weekly LLM
// surfaces for a single week. Themes are not persisted as a taxonomy —
// they're regenerated each week from the day's drainer/charger tags.
type SummaryTheme struct {
	Name         string   `json:"name"`
	Tags         []string `json:"tags"`
	Role         string   `json:"role"` // "drainer" | "charger" | "mixed"
	DaysAppeared int      `json:"days_appeared"`
	Note         string   `json:"note"`
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
