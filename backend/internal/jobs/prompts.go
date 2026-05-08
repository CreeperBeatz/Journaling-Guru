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
// only role now is to produce a single-sentence headline insight that
// feeds Zone 1 of the always-available summary page.
//
// The full headline rewrite (queried tag table input + 1-sentence
// output) is a Phase 6 task; this stub keeps the existing weekly path
// alive on the new schema until then.
const weeklySystemPrompt = `You are a thoughtful journal companion. Write a brief weekly reflection
from the user's check-ins below. Surface the throughline — the recurring
drainer or charger, or the emotional shape of the week. Avoid clichés.

Respond with markdown only — no JSON, no top-level heading. Target 60–120
words. (A future revision will demote this to a single-sentence headline
insight; for now keep the short paragraph form.)`

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

// weeklyDailyChild is the per-day view fed into the weekly template.
// Mood is the 1..3 audit scale; the existing weekly.tmpl renders it as
// a label.
type weeklyDailyChild struct {
	Date      string
	MoodLabel string
}
