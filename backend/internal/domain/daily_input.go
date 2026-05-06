package domain

import "time"

// DailyInput is the user-provided per-day check-in: a 1-10 mood score,
// a list of emotions felt, and freeform notes. Distinct from
// JournalEntry (which is one row per question per day) — exactly one
// DailyInput row per (user, local_date).
//
// MoodScore is a pointer so the API can express "user hasn't set it yet"
// without colliding with a real low value of 0. Wire format: integer
// 1..10 or null. emotions is a string array; notes is plain text.
type DailyInput struct {
	ID         string    `json:"id"`
	UserID     string    `json:"-"`
	LocalDate  string    `json:"local_date"` // YYYY-MM-DD
	MoodScore  *int      `json:"mood_score,omitempty"`
	Emotions   []string  `json:"emotions"`
	Notes      string    `json:"notes"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// MoodLabel maps a 1-10 mood score to one of three labels. Buckets:
//
//	1-4  → "negative"
//	5-6  → "neutral"
//	7-10 → "positive"
//
// Returns "" for nil — callers display this as "—" or omit the chip.
func MoodLabel(score *int) string {
	if score == nil {
		return ""
	}
	switch {
	case *score <= 4:
		return "negative"
	case *score <= 6:
		return "neutral"
	default:
		return "positive"
	}
}
