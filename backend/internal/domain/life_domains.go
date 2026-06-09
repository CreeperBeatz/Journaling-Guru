package domain

import "fmt"

// Life check-in domains — the monthly satisfaction sliders. The set and
// scale follow the Personal Wellbeing Index format (one "How satisfied
// are you with…" item per domain, 0–10 end-defined scale; the
// OECD-recommended format for subjective wellbeing). Domains are the
// consensus core across PWI / Gallup five elements / QOLI / Wheel of
// Life, with warm non-clinical labels.
//
// Keys are STABLE — they live inside monthly_reflections.ratings jsonb
// and the yearly chart depends on them staying comparable across months.
// Never rename a key; add new ones at the end if the set ever grows.
const (
	LifeDomainOverall        = "life_overall"    // global anchor — always rated first
	LifeDomainHealthEnergy   = "health_energy"   // Health & energy
	LifeDomainMindInner      = "mind_inner"      // Mind & inner life
	LifeDomainRelationships  = "relationships"   // Close relationships
	LifeDomainWorkPurpose    = "work_purpose"    // Work & purpose
	LifeDomainMoneySecurity  = "money_security"  // Money & security
	LifeDomainPlayRest       = "play_rest"       // Play & rest
	LifeDomainGrowthLearning = "growth_learning" // Growth & learning
	LifeDomainBelonging      = "belonging"       // Belonging (optional — PWI-style opt-in 8th item)
)

// LifeDomain pairs a stable ratings key with its user-facing label. Order
// matters: the global item comes first (PWI/OECD ordering — domains must
// not prime the headline number), then the seven core domains, then the
// optional one.
type LifeDomain struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Optional bool   `json:"optional"`
}

// LifeDomains is the canonical ordered list. The FE mirrors labels; the
// backend owns validation.
var LifeDomains = []LifeDomain{
	{Key: LifeDomainOverall, Label: "Life as a whole"},
	{Key: LifeDomainHealthEnergy, Label: "Health & energy"},
	{Key: LifeDomainMindInner, Label: "Mind & inner life"},
	{Key: LifeDomainRelationships, Label: "Close relationships"},
	{Key: LifeDomainWorkPurpose, Label: "Work & purpose"},
	{Key: LifeDomainMoneySecurity, Label: "Money & security"},
	{Key: LifeDomainPlayRest, Label: "Play & rest"},
	{Key: LifeDomainGrowthLearning, Label: "Growth & learning"},
	{Key: LifeDomainBelonging, Label: "Belonging", Optional: true},
}

// LifeDomainLabel returns the label for a ratings key, or the key itself
// for unknown values (defensive — prompts should never crash on data).
func LifeDomainLabel(key string) string {
	for _, d := range LifeDomains {
		if d.Key == key {
			return d.Label
		}
	}
	return key
}

// ValidateRatings rejects unknown domain keys and out-of-range scores.
// Partial maps are fine (belonging is opt-in; a skipped check-in is a
// NULL column, not an empty map) — but an empty map is rejected so the
// ratings_set_at stamp always means "the user actually rated something".
func ValidateRatings(ratings map[string]int) error {
	if len(ratings) == 0 {
		return fmt.Errorf("ratings must not be empty")
	}
	for key, score := range ratings {
		known := false
		for _, d := range LifeDomains {
			if d.Key == key {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown life domain %q", key)
		}
		if score < 0 || score > 10 {
			return fmt.Errorf("rating for %q out of range: %d (want 0..10)", key, score)
		}
	}
	return nil
}
