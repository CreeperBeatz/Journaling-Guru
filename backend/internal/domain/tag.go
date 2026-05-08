package domain

import "time"

// Tag is a user-owned, valenced label for a recurring drainer or charger.
// IDs are permanent; rename updates Label only so day → tag history stays
// intact across renames. NormalizedLabel (lower(trim(label))) is the
// uniqueness key per user — it's how the chat extraction worker reuses
// an existing tag instead of creating a duplicate ("scrolling Twitter"
// and "social media" can later be merged via Status='merged' +
// MergedIntoTagID).
type Tag struct {
	ID               string    `json:"id"`
	UserID           string    `json:"-"`
	Label            string    `json:"label"`
	NormalizedLabel  string    `json:"-"`
	Valence          string    `json:"valence"` // "positive" | "negative" | "neutral"
	Status           string    `json:"status"`  // "active" | "merged" | "archived"
	MergedIntoTagID  *string   `json:"merged_into_tag_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// TagRole names which side of the day the tag is attached to. Mirrors
// the daily_entry_tags.role CHECK constraint.
const (
	TagRoleDrainer = "drainer"
	TagRoleCharger = "charger"
)

// TagValence values match the tags.valence CHECK constraint.
const (
	TagValencePositive = "positive"
	TagValenceNegative = "negative"
	TagValenceNeutral  = "neutral"
)
