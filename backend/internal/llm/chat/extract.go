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
// extraction LLM call. Fields mirror the JSON schema in
// chatExtractionSystemPrompt; bounds are enforced after parse.
type ExtractionResult struct {
	MoodScore *int              // 1..10 or nil
	Emotions  []string          // ≤ 8, lowercase, deduped
	Notes     string            // ≤ 400 chars, trimmed
	Answers   map[string]string // question_id → text; omitted-keys absent
}

// extractionMaxTokens caps the response. The schema is small but the
// answers map can grow with the question count; 1500 leaves headroom
// for users with 6+ questions and verbose conversations.
const extractionMaxTokens = 1500

// extractionMaxEmotions / extractionMaxNotes are the post-parse caps.
// Validation drops/trims rather than failing — we'd rather persist a
// trimmed result than reject a 401-char notes field.
const (
	extractionMaxEmotions = 8
	extractionMaxNotes    = 400
)

// ExtractParams bundles the inputs to Extract.
type ExtractParams struct {
	Model     string // per-call override; empty falls back to client default
	Questions []QuestionView
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
	system, user, err := BuildExtractionPrompts(params.Questions, params.Messages)
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
	validated := validateExtraction(parsed, params.Questions)
	return validated, nil
}

// rawExtraction is the wire shape of the LLM's JSON output.
type rawExtraction struct {
	MoodScore *int              `json:"mood_score"`
	Emotions  []string          `json:"emotions"`
	Notes     string             `json:"notes"`
	Answers   map[string]string `json:"answers"`
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
//   - mood_score: out-of-range (anything not 1..10) becomes nil.
//   - emotions: trimmed, lowercased, deduped, capped at 8.
//   - notes: trimmed, capped at 400 chars (with ellipsis on overflow).
//   - answers: only keys matching one of `params.Questions` are kept.
//     Empty/whitespace bodies are dropped.
//
// The returned result is safe to write to daily_inputs and journal_entries.
func validateExtraction(raw *rawExtraction, questions []QuestionView) *ExtractionResult {
	out := &ExtractionResult{
		Answers: map[string]string{},
	}
	if raw.MoodScore != nil && *raw.MoodScore >= 1 && *raw.MoodScore <= 10 {
		score := *raw.MoodScore
		out.MoodScore = &score
	}
	seen := map[string]struct{}{}
	for _, e := range raw.Emotions {
		v := strings.ToLower(strings.TrimSpace(e))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out.Emotions = append(out.Emotions, v)
		if len(out.Emotions) >= extractionMaxEmotions {
			break
		}
	}
	notes := strings.TrimSpace(raw.Notes)
	if len(notes) > extractionMaxNotes {
		notes = notes[:extractionMaxNotes-1] + "…"
	}
	out.Notes = notes

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
	return out
}

// EmotionsToText flattens the extracted emotions list back into the
// `daily_inputs.emotions_text` shape — comma-separated phrases. The
// async EmotionClassifyWorker re-classifies on the next tick.
func (r *ExtractionResult) EmotionsToText() string {
	return strings.Join(r.Emotions, ", ")
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
