package domain

import "time"

// Question is the user-editable prompt that drives DailyEntry. Archived
// rows (archived_at != NULL) are filtered at the store layer; entries
// keep their FK to archived questions so HistoryView still renders.
type Question struct {
	ID         string     `json:"id"`
	UserID     string     `json:"-"`
	Prompt     string     `json:"prompt"`
	Position   int        `json:"position"`
	ArchivedAt *time.Time `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// DefaultQuestions is empty under the Energy Audit pivot — the daily
// flow's five prompts (mood / drained / charged / gratitude /
// reflection) are baked into the DailyInputs surface, not modeled as
// rows in the `questions` table. The table itself remains as
// scaffolding for a future "user adds custom prompts" expansion.
var DefaultQuestions = []string{}
