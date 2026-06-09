package domain

import "time"

// MonthlyReflection is one row of monthly_reflections — the state for a
// user's reflection on a calendar month, hosted inside the combined
// weekly+monthly session on their "monthly day" (first reflection_weekday
// on-or-after month end). One row per (user_id, month_start).
//
// Unlike the weekly wizard there is no step cursor — the hosting weekly
// reflection owns the wizard; this row holds the month-scoped artifacts:
// the direction check, the intention for next month, and the life
// check-in ratings.
type MonthlyReflection struct {
	ID         string `json:"id"`
	UserID     string `json:"-"`
	MonthStart string `json:"month_start"` // YYYY-MM-DD, 1st of month
	MonthEnd   string `json:"month_end"`   // YYYY-MM-DD, last day of month
	// WeekStart anchors the hosting weekly reflection. Re-anchors on
	// carry-over when the user does the monthly a week late.
	WeekStart *string `json:"week_start,omitempty"`
	// ChatSessionID points at the combined weekly chat session.
	ChatSessionID *string `json:"chat_session_id,omitempty"`
	// DirectionText is distilled at finalize: did the month move the user
	// toward the life they want?
	DirectionText string `json:"direction_text"`
	// IntentionText is the ONE intention/theme for next month — broader
	// than a weekly tiny goal ("protect my mornings"). A user artifact:
	// replay preserves it.
	IntentionText  string     `json:"intention_text"`
	IntentionSetAt *time.Time `json:"intention_set_at,omitempty"`
	// Ratings is the life check-in: domain key → 0..10 (see LifeDomains).
	// Nil until submitted; the check-in is skippable.
	Ratings map[string]int `json:"ratings,omitempty"`
	// RatingNotes is the optional per-domain explanation ("why this
	// score?"), same keys as Ratings. Absent key = no note.
	RatingNotes  map[string]string `json:"rating_notes,omitempty"`
	RatingsSetAt *time.Time        `json:"ratings_set_at,omitempty"`
	CompletedAt  *time.Time        `json:"completed_at"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}
