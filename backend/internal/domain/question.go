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

// DefaultQuestions seeds a new account on first /api/questions GET. Order
// here matches the order they'll be presented in DailyEntry.
var DefaultQuestions = []string{
	"What stood out about today?",
	"What are you grateful for?",
	"What's on your mind right now?",
	"What did you learn today?",
	"What's one thing you'd like to do better tomorrow?",
}
