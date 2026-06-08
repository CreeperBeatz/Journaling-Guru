package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
)

// ExtractionResult is the validated output of the single-shot
// extraction LLM call under the Energy Audit pivot. Fields mirror the
// JSON schema in chatExtractionSystemPrompt; bounds are enforced after
// parse.
//
// Tag proposals (drainer/charger labels) are short, lowercased phrases
// that the worker reconciles against the user's existing tag list via
// TagStore.UpsertByLabel before writing daily_entry_tags rows. Labels
// here are pre-normalization — the store owns the dedup rule.
type ExtractionResult struct {
	Mood                  *int              // signed -2..2 or nil
	DrainedText           string            // ≤ 1000 chars
	ChargedText           string            // ≤ 1000 chars
	GratitudeText         string            // ≤ 1000 chars
	ReflectionText        string            // ≤ 4000 chars
	DrainedTagProposals   []string          // ≤ 5 short labels for negative tags
	ChargedTagProposals   []string          // ≤ 5 short labels for positive tags
	Answers               map[string]string // question_id → text; omitted-keys absent
	GoalCheckIns          []GoalCheckInExtraction
}

// GoalCheckInExtraction is one yes/no answer to a goal's daily check-in
// question, surfaced from the transcript. Apply step skips entries whose
// goal_id already has a manual check-in row for the day (manual-wins).
type GoalCheckInExtraction struct {
	GoalID string
	Value  bool
}

// extractionMaxTokens caps the response. The schema is small but the
// answers map can grow with the question count; 1500 leaves headroom
// for users with 6+ questions and verbose conversations.
const extractionMaxTokens = 1500

// Per-field caps. Validation trims rather than fails — we'd rather
// persist a trimmed result than reject a slightly-too-long field.
const (
	extractionMaxAuditText      = 1000
	extractionMaxReflectionText = 4000
	extractionMaxTagsPerRole    = 5
	extractionMaxTagLabel       = 60 // matches store/tags maxTagLabelLen
)

// ExtractParams bundles the inputs to Extract.
type ExtractParams struct {
	Model     string // per-call override; empty falls back to client default
	Questions []QuestionView
	Goals     []GoalView
	Messages  []domain.ChatMessage
}

// Extract runs the extraction LLM call and returns a validated result.
// JSON-mode is on; the response is parsed, validated, and clamped to
// the documented bounds. Returns an error if the LLM emits malformed
// JSON or zero turns of usable transcript exist.
func Extract(
	ctx context.Context,
	client *llm.OpenRouter,
	params ExtractParams,
) (*ExtractionResult, error) {
	lines := TranscriptLinesFromMessages(params.Messages)
	if len(lines) == 0 {
		return nil, errors.New("extract: empty transcript")
	}
	system, user, err := BuildExtractionPrompts(params.Questions, params.Goals, params.Messages)
	if err != nil {
		return nil, err
	}
	resp, err := client.Complete(ctx, llm.CompletionRequest{
		Model:     params.Model,
		System:    system,
		User:      user,
		MaxTokens: extractionMaxTokens,
		JSONMode:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("extraction llm call: %w", err)
	}
	parsed, err := parseExtractionJSON(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("parse extraction response: %w (content: %s)", err, truncate(resp.Content, 300))
	}
	validated := validateExtraction(parsed, params.Questions, params.Goals)
	return validated, nil
}

// rawExtraction is the wire shape of the LLM's JSON output.
type rawExtraction struct {
	Mood                *int              `json:"mood"`
	DrainedText         string            `json:"drained_text"`
	ChargedText         string            `json:"charged_text"`
	GratitudeText       string            `json:"gratitude_text"`
	ReflectionText      string            `json:"reflection_text"`
	DrainedTagProposals []string          `json:"drained_tag_proposals"`
	ChargedTagProposals []string          `json:"charged_tag_proposals"`
	Answers             map[string]string `json:"answers"`
	GoalCheckIns        []rawGoalCheckIn  `json:"goal_check_ins"`
}

type rawGoalCheckIn struct {
	GoalID string `json:"goal_id"`
	Value  bool   `json:"value"`
}

func parseExtractionJSON(content string) (*rawExtraction, error) {
	cleaned := stripFences(content)
	var out rawExtraction
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// validateExtraction clamps and de-hallucinates the raw LLM output:
//   - mood: out-of-range (anything not -2..2) becomes nil.
//   - drained/charged/gratitude_text: trimmed, capped at 1000 chars.
//   - reflection_text: trimmed, capped at 4000 chars.
//   - answers: only keys matching one of `questions` are kept.
//     Empty/whitespace bodies are dropped.
//   - goal_check_ins: only entries whose goal_id matches one of `goals`
//     are kept; later occurrences of the same goal_id overwrite earlier
//     ones (the apply step then skips entirely if a manual row exists).
//
// The returned result is safe to feed into ApplyExtraction.
func validateExtraction(raw *rawExtraction, questions []QuestionView, goals []GoalView) *ExtractionResult {
	out := &ExtractionResult{
		Answers:      map[string]string{},
		GoalCheckIns: []GoalCheckInExtraction{},
	}
	if raw.Mood != nil && *raw.Mood >= -2 && *raw.Mood <= 2 {
		m := *raw.Mood
		out.Mood = &m
	}
	out.DrainedText = clampText(raw.DrainedText, extractionMaxAuditText)
	out.ChargedText = clampText(raw.ChargedText, extractionMaxAuditText)
	out.GratitudeText = clampText(raw.GratitudeText, extractionMaxAuditText)
	out.ReflectionText = clampText(raw.ReflectionText, extractionMaxReflectionText)
	out.DrainedTagProposals = clampTagList(raw.DrainedTagProposals)
	out.ChargedTagProposals = clampTagList(raw.ChargedTagProposals)

	validIDs := map[string]struct{}{}
	for _, q := range questions {
		validIDs[q.ID] = struct{}{}
	}
	for qid, body := range raw.Answers {
		if _, ok := validIDs[qid]; !ok {
			continue
		}
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		out.Answers[qid] = body
	}

	validGoalIDs := map[string]struct{}{}
	for _, g := range goals {
		validGoalIDs[g.ID] = struct{}{}
	}
	// Dedupe by goal_id; last occurrence wins. Order is preserved by the
	// goal's first appearance, which is fine for the apply loop.
	dedup := make(map[string]int, len(raw.GoalCheckIns))
	for _, gc := range raw.GoalCheckIns {
		if _, ok := validGoalIDs[gc.GoalID]; !ok {
			continue
		}
		if idx, exists := dedup[gc.GoalID]; exists {
			out.GoalCheckIns[idx].Value = gc.Value
			continue
		}
		dedup[gc.GoalID] = len(out.GoalCheckIns)
		out.GoalCheckIns = append(out.GoalCheckIns, GoalCheckInExtraction{
			GoalID: gc.GoalID,
			Value:  gc.Value,
		})
	}
	return out
}

func clampText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	return s
}

// clampTagList trims labels, drops empty, dedupes by lowercased value,
// caps at extractionMaxTagsPerRole, and clamps each label to the
// extractionMaxTagLabel cap.
func clampTagList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if len(s) > extractionMaxTagLabel {
			s = s[:extractionMaxTagLabel]
		}
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, s)
		if len(out) >= extractionMaxTagsPerRole {
			break
		}
	}
	return out
}

// stripFences removes ```json ... ``` wrappers some models still emit
// even when asked for raw JSON. Idempotent on un-fenced input.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.Index(s, "\n"); nl >= 0 {
		s = s[nl+1:]
	}
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// weeklySurpriseMaxChars caps the distilled continuity paragraph.
// Matches the JSON schema in weeklySurpriseExtractSystemPrompt.
const weeklySurpriseMaxChars = 1200

// ExtractWeeklySurpriseParams bundles the inputs for the post-wrap-up
// continuity extract. Model defaults to the OpenRouter client's default
// (CLASSIFY_MODEL tier); callers can override.
type ExtractWeeklySurpriseParams struct {
	Model    string
	Messages []domain.ChatMessage
}

// ExtractWeeklySurprise runs the post-wrap-up extract on a weekly chat
// transcript and returns one short sentence capturing what next week's
// letter should remember. Empty output is a valid result. Errors only
// for transport/parse failures — empty transcript returns "" with nil
// err so finalize doesn't block on extract.
func ExtractWeeklySurprise(
	ctx context.Context,
	client *llm.OpenRouter,
	params ExtractWeeklySurpriseParams,
) (string, error) {
	lines := TranscriptLinesFromMessages(params.Messages)
	if len(lines) == 0 {
		return "", nil
	}
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(strings.ToUpper(l.Role[:1]))
		sb.WriteString(l.Role[1:])
		sb.WriteString(": ")
		sb.WriteString(l.Content)
		sb.WriteString("\n")
	}
	resp, err := client.Complete(ctx, llm.CompletionRequest{
		Model:     params.Model,
		System:    weeklySurpriseExtractSystemPrompt,
		User:      sb.String(),
		MaxTokens: 700,
		JSONMode:  true,
	})
	if err != nil {
		return "", fmt.Errorf("weekly surprise extract: %w", err)
	}
	var parsed struct {
		Surprise string `json:"surprise"`
	}
	if err := json.Unmarshal([]byte(stripFences(resp.Content)), &parsed); err != nil {
		return "", fmt.Errorf("parse weekly surprise: %w (content: %s)", err, truncate(resp.Content, 200))
	}
	s := strings.TrimSpace(parsed.Surprise)
	if len(s) > weeklySurpriseMaxChars {
		s = s[:weeklySurpriseMaxChars-1] + "…"
	}
	return s, nil
}
