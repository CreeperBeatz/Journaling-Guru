package jobs

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/cosmosthrace/journai/backend/internal/llm/prompts"
)

// System prompts for each period type. Kept as code constants (not in
// the .tmpl files) so the user-facing template stays focused on the
// rendered context, and so that a future "per-user tone" pref can compose
// onto the system prompt without re-parsing templates.

const dailySystemPrompt = `You are a thoughtful journal companion who reflects back on the user's day.
Write in second person ("you reflected on…", "you noticed…"). Keep tone warm
but honest — name what's there, don't sugarcoat or moralize. Avoid clichés.

You MUST respond with a single JSON object — no prose before or after, no
markdown fences. The schema is:
{
  "body":        string,    // markdown reflection, 80–180 words
  "emotions":    string[],  // 1–6 distinct lower-case emotion words felt today
  "mood_score":  number,    // overall mood 1–10 (1 = very negative, 10 = very positive)
  "mood_label":  string,    // one of: "negative", "neutral", "positive"
  "topics":      string[]   // 1–5 lower-case topic tags (e.g. "work", "family", "sleep")
}`

const weeklySystemPrompt = `You are a thoughtful journal companion. Write a weekly reflection from the
daily summaries below. Tone: warm, honest, second person. Surface throughlines
(recurring topics, emotional arcs, contrasts between days) — don't just
enumerate the days. Avoid clichés and motivational filler.

Respond with markdown only — no JSON, no top-level heading, just 2–4 short
paragraphs. Target 150–250 words.`

const monthlySystemPrompt = `You are a thoughtful journal companion. Write a monthly reflection from the
weekly summaries below. Look for the shape of the month — what shifted, what
stayed constant, what the user kept returning to. Tone: warm, honest, second
person.

Respond with markdown only — no JSON, no top-level heading, just 3–5 short
paragraphs. Target 250–400 words.`

const yearlySystemPrompt = `You are a thoughtful journal companion. Write a year-in-review from the
monthly summaries below. This is the longest reflection — name the major
chapters of the year, the people who recurred, the shifts the user moved
through. Tone: warm, honest, second person, reflective rather than
celebratory.

Respond with markdown only — no JSON. You may use up to 4 short ## subheadings
if it helps structure the year (e.g. "Spring", "What stayed", "What shifted")
but they're optional. Target 400–700 words.`

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

// dailyEntry / weeklyChild / monthlyChild are the per-period view shapes
// fed into templates. Decoupled from store types so the template surface
// stays minimal.
type dailyEntry struct {
	Prompt string
	Body   string
}

type weeklyDailyChild struct {
	Date      string
	Body      string
	MoodLabel string
	MoodScore string
	Topics    string
}

type monthlyWeeklyChild struct {
	PeriodStart string
	Body        string
}

type yearlyMonthlyChild struct {
	MonthLabel string
	Body       string
}
