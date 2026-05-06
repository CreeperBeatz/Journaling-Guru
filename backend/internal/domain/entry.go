package domain

import "time"

// JournalEntry is one user-authored answer to a question for a single
// calendar day in the user's timezone.
//
// LocalDate is wire-formatted as YYYY-MM-DD; the Go zero-time bookkeeping
// of time.Time is hidden behind a custom marshaler in the API layer.
type JournalEntry struct {
	ID             string    `json:"id"`
	UserID         string    `json:"-"`
	QuestionID     string    `json:"question_id"`
	LocalDate      string    `json:"local_date"` // YYYY-MM-DD
	Body           string    `json:"body"`
	Source         string    `json:"source"`
	VoiceSessionID *string   `json:"voice_session_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
