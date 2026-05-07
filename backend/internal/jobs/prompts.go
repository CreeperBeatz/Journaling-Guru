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

The user provides their own mood and emotions in the check-in section
(when present); your job is to write the prose body and extract topics.
Do NOT contradict the user's own reading of their day — if they say their
mood was 3 and felt anxious, that's the truth, even if their answers read
otherwise on the surface.

You MUST respond with a single JSON object — no prose before or after, no
markdown fences. The schema is:
{
  "body":   string,    // markdown reflection, 80–180 words
  "topics": string[]   // 1–5 lower-case topic tags (e.g. "work", "family", "sleep")
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

// emotionClassifySystemPrompt drives the EmotionClassifyWorker. The 24
// (base, subtype) pairs enumerated here are the exhaustive vocabulary —
// the worker drops any entry not in domain.PlutchikSubtypes. We want the
// model to refuse rather than invent for borderline phrases ("had pasta
// for lunch" is not an emotion).
const emotionClassifySystemPrompt = `You classify free-text emotion descriptions into Plutchik's wheel.

The 24 valid (base, subtype) pairs — and the ONLY values you may emit:

  joy:          serenity (mild), joy (medium), ecstasy (intense)
  trust:        acceptance, trust, admiration
  fear:         apprehension, fear, terror
  surprise:     distraction, surprise, amazement
  sadness:      pensiveness, sadness, grief
  disgust:      boredom, disgust, loathing
  anger:        annoyance, anger, rage
  anticipation: interest, anticipation, vigilance

Rules:
- Only emit (base, subtype) pairs from the list above. Never invent
  subtypes or use synonyms outside the list.
- One classified entry per distinct emotion. If the user expresses the
  same feeling multiple ways, pick the strongest phrasing.
- Cap output at 6 entries, most salient first.
- raw_phrase must be a verbatim slice from the user's text — do not
  paraphrase, summarize, or translate.
- If the text contains no recognizable emotion, return an empty array.

Respond with a single JSON object — no prose before or after, no
markdown fences:

{
  "emotions": [
    {"raw_phrase": "...", "base": "joy", "subtype": "ecstasy"}
  ]
}`

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
