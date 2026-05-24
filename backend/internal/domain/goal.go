package domain

import "time"

// Goal closes the loop between "spotted a pattern" and "tried to change
// something." Each active goal contributes one yes/no daily check-in to
// the daily flow. End date is required (spec: prevents runaway goals);
// at end_date a wrap-up flow asks the user to record kept/dropped/
// inconclusive.
type Goal struct {
	ID                string     `json:"id"`
	UserID            string     `json:"-"`
	Title             string     `json:"title"`
	CheckInQuestion   string     `json:"check_in_question"`
	// WhyMatters / IfFollowed / IfNotFollowed are the user's own words on
	// the motivation behind the goal — captured by the weekly reflection
	// companion before it calls propose_goal. Empty for goals created
	// manually (Goals page) or via the SMART shaper. See migration
	// 0022_goal_motivation.sql.
	WhyMatters        string     `json:"why_matters"`
	IfFollowed        string     `json:"if_followed"`
	IfNotFollowed     string     `json:"if_not_followed"`
	StartDate         string     `json:"start_date"`         // YYYY-MM-DD
	EndDate           string     `json:"end_date"`           // YYYY-MM-DD
	Status            string     `json:"status"`             // "active" | "completed" | "abandoned"
	Outcome           *string    `json:"outcome,omitempty"`  // "kept" | "dropped" | "inconclusive"
	ConclusionText    string     `json:"conclusion_text"`
	CreatedAt         time.Time  `json:"created_at"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
}

// GoalCheckIn is a yes/no answer to a goal's check_in_question on a given
// local_date. (goal_id, local_date) is the idempotency anchor so the
// daily flow can re-save without creating duplicates.
type GoalCheckIn struct {
	GoalID    string    `json:"goal_id"`
	LocalDate string    `json:"local_date"` // YYYY-MM-DD
	Value     bool      `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Goal status values match the goals.status CHECK constraint.
const (
	GoalStatusActive    = "active"
	GoalStatusCompleted = "completed"
	GoalStatusAbandoned = "abandoned"
)

// Goal outcome values match the goals.outcome CHECK constraint.
const (
	GoalOutcomeKept         = "kept"
	GoalOutcomeDropped      = "dropped"
	GoalOutcomeInconclusive = "inconclusive"
)
