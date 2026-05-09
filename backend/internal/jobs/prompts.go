package jobs

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/cosmosthrace/journai/backend/internal/llm/prompts"
)

// weeklySystemPrompt is the surviving summary prompt under the Energy
// Audit pivot. The daily / monthly / yearly LLM summaries are retired
// (spec: "no daily AI summary, no AI recap"). The weekly summary's
// sole role is to produce a single-sentence headline insight that
// feeds Zone 1 of the always-available summary page.
//
// The model receives a tag-table snapshot of the week (top drainers
// + chargers with appearance counts and avg-mood-on-those-days) plus
// the mood-by-day series. It MUST emit one sentence that names the
// most informative pattern — or honestly says there isn't one yet.
const weeklySystemPrompt = `You write a single-sentence headline insight from a week of journal
data. You are not a coach; you are a precise pattern-spotter.

# Output

Exactly ONE sentence. ≤ 28 words. Plain prose, no markdown, no JSON,
no quotes around the sentence. Do NOT include "this week" if the data
already implies it. Do NOT use second person ("you") unless it reads
naturally; impersonal is fine.

# Rules

- Surface the most informative pattern visible in the table the user
  shows you. Strong patterns:
    - A drainer or charger with the highest appearance count AND a
      clearly different avg-mood from the week's average.
    - A clear best/worst day with a notable charger or drainer.
    - A streak (mood up multiple days in a row, etc.).
- If no pattern stands out — too few data points, or the spread is
  flat — say so honestly. Examples:
    - "Quiet, middle-of-the-road week with no clear pattern yet."
    - "Mood held steady; the data is still thin to call a trend."
- Never invent or extrapolate beyond what the table shows. No advice,
  no "try X next week" — just the observation.
- Avoid clichés ("ups and downs", "highs and lows", "rollercoaster").`

// renderTemplate compiles and executes one of the embedded .tmpl files
// against `data`. We re-parse on each call — the templates are small and
// the worker is not hot enough to warrant caching.
func renderTemplate(name string, data any) (string, error) {
	raw, err := prompts.FS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	t, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute %s: %w", name, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

