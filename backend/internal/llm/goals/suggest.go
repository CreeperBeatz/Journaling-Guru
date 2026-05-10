package goals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cosmosthrace/journai/backend/internal/llm"
)

// SuggestSystemPrompt asks the model for 3 SMART, low-friction goal
// candidates derived from the user's recent week pattern. Output is
// strict JSON; the handler validates each candidate before returning.
//
// "Low friction" is the load-bearing word: defaults to 2-week duration,
// check-in answerable in <5s, and the title should suggest a tiny
// behavior change (not a value statement). Goals always end on the
// user's reflection_weekday — the FE's create flow handles the snap.
const SuggestSystemPrompt = `You propose 3 SMART, low-friction goal candidates for a user
based on the energy-audit pattern from their past week.

Each candidate is JSON:
  {
    "title": short, concrete, action-shaped (NOT a value statement)
    "check_in_question": yes/no, answerable in <5 seconds
    "duration_weeks": integer 1..4 (default 2)
    "rationale": one sentence connecting it to a drainer/charger
  }

# Rules
- Output JSON ONLY. No prose. Schema:
  {"suggestions":[{...},{...},{...}]}
- Title ≤ 60 chars. check_in_question ≤ 100 chars.
- The check-in must be measurable. "Did you take a walk after lunch?" yes.
  "Did you feel happier?" no.
- Tie at least 2 of the 3 directly to the strongest signals in the
  pattern (top drainer or top charger).
- Default duration_weeks = 2. Use 1 only when the change is tiny and
  daily; use 3 or 4 only when habit formation needs runway.
- Don't recommend abstinence-only goals ("never use phone after 22:00")
  if a positive substitute is plausible ("read 10 pages before bed").
- Don't repeat any of the user's currently active goals.`

// SuggestionInput is the runtime context passed into the prompt. The
// handler fills it from the same store calls the WeeklyReflection
// endpoint uses.
type SuggestionInput struct {
	TopDrainers    []TagPattern
	TopChargers    []TagPattern
	MoodAvg        *float64
	ActiveGoals    []string // titles, so the LLM avoids dupes
	WeekDays       int
}

type TagPattern struct {
	Label       string
	Appearances int
	AvgMood     *float64
}

// Suggestion is one candidate. Mirrors the JSON contract above.
type Suggestion struct {
	Title           string `json:"title"`
	CheckInQuestion string `json:"check_in_question"`
	DurationWeeks   int    `json:"duration_weeks"`
	Rationale       string `json:"rationale"`
}

// Suggest calls the LLM for 3 candidates. Returns up to 3, validated:
// titles + questions trimmed and bounded; duration_weeks clamped to
// 1..4. On parse error returns the parse error so the handler can
// 502 — empty candidates list is a valid (cold-start) response.
func Suggest(
	ctx context.Context, client *llm.OpenRouter, model string, in SuggestionInput,
) ([]Suggestion, error) {
	if client == nil {
		return nil, errors.New("nil llm client")
	}
	user := buildSuggestUserPrompt(in)
	resp, err := client.Complete(ctx, llm.CompletionRequest{
		Model:     model,
		System:    SuggestSystemPrompt,
		User:      user,
		MaxTokens: 700,
		JSONMode:  true,
	})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Suggestions []Suggestion `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &parsed); err != nil {
		return nil, fmt.Errorf("parse suggestions: %w", err)
	}
	out := make([]Suggestion, 0, len(parsed.Suggestions))
	for _, s := range parsed.Suggestions {
		title := strings.TrimSpace(s.Title)
		question := strings.TrimSpace(s.CheckInQuestion)
		rationale := strings.TrimSpace(s.Rationale)
		if title == "" || question == "" {
			continue
		}
		if len(title) > 200 {
			title = title[:200]
		}
		if len(question) > 200 {
			question = question[:200]
		}
		dur := s.DurationWeeks
		if dur < 1 {
			dur = 2
		}
		if dur > 4 {
			dur = 4
		}
		out = append(out, Suggestion{
			Title:           title,
			CheckInQuestion: question,
			DurationWeeks:   dur,
			Rationale:       rationale,
		})
		if len(out) >= 3 {
			break
		}
	}
	return out, nil
}

func buildSuggestUserPrompt(in SuggestionInput) string {
	var b strings.Builder
	b.WriteString("WEEK PATTERN\n")
	b.WriteString(fmt.Sprintf("- Window: last %d days\n", in.WeekDays))
	if in.MoodAvg != nil {
		b.WriteString(fmt.Sprintf("- Mood avg: %.1f / 3\n", *in.MoodAvg))
	} else {
		b.WriteString("- Mood avg: (not enough data)\n")
	}
	b.WriteString("- Top drainers:\n")
	if len(in.TopDrainers) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, t := range in.TopDrainers {
			b.WriteString(fmt.Sprintf("  - %s (%d days", t.Label, t.Appearances))
			if t.AvgMood != nil {
				b.WriteString(fmt.Sprintf(", mood %.1f", *t.AvgMood))
			}
			b.WriteString(")\n")
		}
	}
	b.WriteString("- Top chargers:\n")
	if len(in.TopChargers) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, t := range in.TopChargers {
			b.WriteString(fmt.Sprintf("  - %s (%d days", t.Label, t.Appearances))
			if t.AvgMood != nil {
				b.WriteString(fmt.Sprintf(", mood %.1f", *t.AvgMood))
			}
			b.WriteString(")\n")
		}
	}
	if len(in.ActiveGoals) > 0 {
		b.WriteString("- Already active goals (avoid repeating):\n")
		for _, g := range in.ActiveGoals {
			b.WriteString("  - " + g + "\n")
		}
	}
	b.WriteString("\nReturn 3 candidates as JSON.")
	return b.String()
}
