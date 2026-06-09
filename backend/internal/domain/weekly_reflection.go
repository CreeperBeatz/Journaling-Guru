package domain

import "time"

// WeeklyReflection is one row of weekly_reflections — the wizard state
// for a user's reflection on the week ending on their reflection_weekday.
// One row per (user_id, week_start). GoalNotes is keyed by goal_id and
// records the optional "how's it going so far?" answer for each
// mid-flight goal that wasn't resolved on this card.
type WeeklyReflection struct {
	ID           string            `json:"id"`
	UserID       string            `json:"-"`
	WeekStart    string            `json:"week_start"` // YYYY-MM-DD
	WeekEnd      string            `json:"week_end"`   // YYYY-MM-DD
	SurpriseText string            `json:"surprise_text"`
	Step         int               `json:"step"`
	GoalNotes    map[string]string `json:"goal_notes"`
	NewGoalIDs   []string          `json:"new_goal_ids"`
	// ChatSessionID points at the weekly-scoped chat_sessions row backing
	// step 2 of the wizard. Set once on first /chat create-or-resume.
	ChatSessionID *string    `json:"chat_session_id,omitempty"`
	CompletedAt   *time.Time `json:"completed_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
