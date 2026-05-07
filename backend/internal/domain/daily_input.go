package domain

import "time"

// DailyInput is the user-provided per-day check-in: a 1-10 mood score,
// a free-text emotions description, and freeform notes. Distinct from
// JournalEntry (which is one row per question per day) — exactly one
// DailyInput row per (user, local_date).
//
// MoodScore is a pointer so the API can express "user hasn't set it yet"
// without colliding with a real low value of 0. Wire format: integer
// 1..10 or null.
//
// EmotionsText is the raw free-text the user typed; ClassifiedEmotions
// is the LLM-derived structured form (Plutchik base + subtype + raw
// phrase). The classifier runs asynchronously in a River worker, so
// ClassifiedEmotions can lag EmotionsText by a few seconds. Summaries
// and stats consume ClassifiedEmotions; DailyInputs UI shows only the
// raw text back to the user.
type DailyInput struct {
	ID                 string              `json:"id"`
	UserID             string              `json:"-"`
	LocalDate          string              `json:"local_date"` // YYYY-MM-DD
	MoodScore          *int                `json:"mood_score,omitempty"`
	EmotionsText       string              `json:"emotions_text"`
	ClassifiedEmotions []ClassifiedEmotion `json:"classified_emotions"`
	Notes              string              `json:"notes"`
	CreatedAt          time.Time           `json:"created_at"`
	UpdatedAt          time.Time           `json:"updated_at"`
}

// ClassifiedEmotion is one entry from the LLM classifier output. Base
// is one of the 8 Plutchik base emotions; Subtype is one of the 24
// intensity-leveled subtypes (mild/medium/intense). RawPhrase is a
// verbatim slice of the user's EmotionsText so the user can later see
// "you said 'jittery before the standup' → fear (apprehension)".
type ClassifiedEmotion struct {
	Base      string `json:"base"`
	Subtype   string `json:"subtype"`
	RawPhrase string `json:"raw_phrase"`
}

// PlutchikSubtypes is the canonical wheel — single source of truth used
// both for prompting the LLM (the system prompt enumerates these pairs)
// and for validating its output. Each base has three intensities in
// mild → medium → intense order.
var PlutchikSubtypes = map[string][3]string{
	"joy":          {"serenity", "joy", "ecstasy"},
	"trust":        {"acceptance", "trust", "admiration"},
	"fear":         {"apprehension", "fear", "terror"},
	"surprise":     {"distraction", "surprise", "amazement"},
	"sadness":      {"pensiveness", "sadness", "grief"},
	"disgust":      {"boredom", "disgust", "loathing"},
	"anger":        {"annoyance", "anger", "rage"},
	"anticipation": {"interest", "anticipation", "vigilance"},
}

// IsValidPlutchik reports whether (base, subtype) is one of the 24 valid
// pairs in PlutchikSubtypes. Used to filter LLM output — we'd rather
// drop a bad classification than persist an invented subtype.
func IsValidPlutchik(base, subtype string) bool {
	subs, ok := PlutchikSubtypes[base]
	if !ok {
		return false
	}
	for _, s := range subs {
		if s == subtype {
			return true
		}
	}
	return false
}

// EmotionClassifyJob is the scheduler-queue row for the async Plutchik
// classifier. Mirrors SummaryJob's lifecycle (pending → claimed →
// completed/skipped/failed) but is keyed by (user_id, local_date) since
// classification is per-day and not per-period.
type EmotionClassifyJob struct {
	ID         string     `json:"id"`
	UserID     string     `json:"-"`
	LocalDate  string     `json:"local_date"`
	FireAt     time.Time  `json:"fire_at"`
	FiredAt    *time.Time `json:"fired_at,omitempty"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	LastError  *string    `json:"last_error,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
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
