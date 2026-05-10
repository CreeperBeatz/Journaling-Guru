package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/cosmosthrace/journai/backend/internal/llm"
)

// MergeEntry combines an existing manual journal-entry body with a
// freshly-extracted body for the same question into one coherent
// first-person entry. Used by the chat extraction worker when the
// user already wrote something for a question and we'd otherwise have
// to choose between clobber and skip.
//
// On any LLM error or empty response, falls back to a lossless append
// (existing + blank line + chat). Worker logs the error and continues.
func MergeEntry(
	ctx context.Context,
	client *llm.OpenRouter,
	model, question, existing, chat string,
) string {
	existing = strings.TrimSpace(existing)
	chat = strings.TrimSpace(chat)
	switch {
	case existing == "":
		return chat
	case chat == "":
		return existing
	}
	merged, err := mergeViaLLM(ctx, client, model, question, existing, chat)
	if err == nil && strings.TrimSpace(merged) != "" {
		return strings.TrimSpace(merged)
	}
	return existing + "\n\n" + chat
}

// MergeText is the same as MergeEntry but for free-form daily-input
// fields (drained_text, charged_text, etc.) where there's no question
// prompt. The label argument names the slot ("what drained you today")
// so the model has a hint about the intended content.
func MergeText(
	ctx context.Context,
	client *llm.OpenRouter,
	model, label, existing, chat string,
) string {
	existing = strings.TrimSpace(existing)
	chat = strings.TrimSpace(chat)
	switch {
	case existing == "":
		return chat
	case chat == "":
		return existing
	}
	merged, err := mergeViaLLM(ctx, client, model, label, existing, chat)
	if err == nil && strings.TrimSpace(merged) != "" {
		return strings.TrimSpace(merged)
	}
	return existing + "\n\n" + chat
}

const mergeSystemPrompt = `You merge two short journal passages about the same prompt into a single coherent first-person passage.

Rules:
- Preserve every distinct fact, feeling, and detail from both inputs. Do not drop content.
- Resolve duplicates and overlaps; keep the writer's first-person voice.
- No introductions, no meta commentary, no headings. Output ONLY the merged passage.
- If both inputs disagree on a detail, keep both phrasings (the writer can edit).
- Match the existing tone and length proportionally — a one-liner + a paragraph should not become an essay.`

func mergeViaLLM(
	ctx context.Context,
	client *llm.OpenRouter,
	model, prompt, existing, chat string,
) (string, error) {
	if client == nil {
		return "", fmt.Errorf("merge: nil client")
	}
	user := fmt.Sprintf("Prompt: %s\n\n--- EXISTING ENTRY ---\n%s\n\n--- NEW PASSAGE FROM TODAY'S CONVERSATION ---\n%s\n\nReturn the merged passage, nothing else.",
		strings.TrimSpace(prompt), existing, chat)
	resp, err := client.Complete(ctx, llm.CompletionRequest{
		Model:     model,
		System:    mergeSystemPrompt,
		User:      user,
		MaxTokens: 1200,
	})
	if err != nil {
		return "", err
	}
	return resp.Content, nil
}
