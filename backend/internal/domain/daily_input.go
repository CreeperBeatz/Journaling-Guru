package domain

import "time"

// DailyInput is the user-provided per-day check-in. Under the Energy
// Audit pivot it carries the five-prompt template's analyzable fields:
// a 1..3 mood scale, the drainer/charger/gratitude/reflection texts.
// Drainer and charger *tags* live in daily_entry_tags, keyed by
// (user_id, local_date, tag_id, role) — they are not on this struct.
//
// Mood is a pointer so the API can express "user hasn't set it yet"
// without colliding with a real low value of 0. Wire format: integer
// 1..3 or null. (1=sad, 2=neutral, 3=happy.)
//
// Backfilled flags entries written ≤2-3 days late so analysis can treat
// them separately if needed (per spec). EditedAt is set whenever a row
// is overwritten after its initial CreatedAt.
type DailyInput struct {
	ID             string     `json:"id"`
	UserID         string     `json:"-"`
	LocalDate      string     `json:"local_date"` // YYYY-MM-DD
	Mood           *int       `json:"mood,omitempty"`
	DrainedText    string     `json:"drained_text"`
	ChargedText    string     `json:"charged_text"`
	GratitudeText  string     `json:"gratitude_text"`
	ReflectionText string     `json:"reflection_text"`
	Backfilled     bool       `json:"backfilled"`
	EditedAt       *time.Time `json:"edited_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// MoodLabel maps the 1..3 scale to a human label. Returns "" for nil so
// callers display "—" or omit the chip.
func MoodLabel(mood *int) string {
	if mood == nil {
		return ""
	}
	switch *mood {
	case 1:
		return "sad"
	case 2:
		return "neutral"
	case 3:
		return "happy"
	}
	return ""
}
