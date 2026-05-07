package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
)

// coverageMaxTokens caps the classifier response. Output is now a
// short list of Q-indices (e.g. ["Q1","Q3"]) instead of full UUIDs,
// so 80 tokens is plenty even for 10+ questions and leaves the model
// no room to ramble. Latency-sensitive: this fires after every
// assistant turn.
const coverageMaxTokens = 80

// coverageRecentWindow is how many trailing chat_messages rows the
// classifier sees. Coverage is judged in delta mode against the
// previously-covered set, so the model only needs the latest few
// turns of context to decide what to add — not the whole transcript.
// User+assistant pairs only; tool/system_event rows are dropped before
// counting (see TranscriptLinesFromMessages).
const coverageRecentWindow = 8

// chatCoverageSystemPrompt drives the post-turn classifier. JSON-mode +
// the classify-tier client (CLASSIFY_MODEL); per-session
// chat_sessions.extraction_model pin overrides per call.
//
// Two key shape changes vs. v1:
//   - Question identifiers are short tokens (Q1, Q2, ...) that the
//     handler maps back to UUIDs. Small models hallucinate and typo
//     long opaque ids; short tokens are reliable.
//   - The classifier runs in delta mode: the prompt includes the set
//     already covered as state, and asks ONLY for newly-covered ids.
//     The handler unions the delta with the prior set.
//
// Anti-hallucination is the priority: unknown ids are dropped post-parse.
const chatCoverageSystemPrompt = `You watch a reflective journal conversation and decide which of the
user's configured questions have JUST become substantively addressed in
the latest turn(s).

You will see:
- A list of active questions, each labelled Q1, Q2, ...
- A list of question ids ALREADY judged covered (state from prior turns).
- The most recent slice of the conversation (oldest → newest).

Output ONE JSON object — no prose before/after, no markdown fences:

{
  "newly_covered": ["Q2", ...]
}

# Rules

- A question is "covered" only when the user has shared something real
  and concrete about it. Passing mentions, deflections, or the
  assistant's prompts on the topic do NOT count.
- Be CONSERVATIVE. If you're not sure, omit. A false negative is
  gentler than a false positive.
- Output ONLY ids that are NOT already in "already covered" — that's the
  delta. Never repeat ids the system already knows about.
- Use ONLY the Q-labels from the active questions list. Never invent
  labels. Never quote prompts back.
- Empty list ([]) is a perfectly valid answer when nothing new landed.`

// CoverageParams bundles inputs to Classify.
//
// PreviouslyCovered is the union persisted on chat_sessions —
// resolved UUIDs, not Q-indices. Classify maps them into Q-labels
// internally before sending to the model.
type CoverageParams struct {
	Model             string // per-call override; empty falls back to client default
	Questions         []QuestionView
	Messages          []domain.ChatMessage
	PreviouslyCovered []string // question UUIDs already covered
}

// Classify runs the post-turn classifier in delta mode and returns the
// FULL covered set (previous union ∪ delta) as resolved question UUIDs.
// Unknown labels emitted by the model are dropped silently.
//
// Returns (nil, nil) when there's nothing usable to classify (no active
// questions, or no user turns yet — typical for the opener path).
func Classify(
	ctx context.Context,
	client *llm.OpenRouter,
	params CoverageParams,
) ([]string, error) {
	if len(params.Questions) == 0 {
		return nil, nil
	}
	hasUser := false
	for _, m := range params.Messages {
		if m.Role == domain.ChatRoleUser && strings.TrimSpace(m.Content) != "" {
			hasUser = true
			break
		}
	}
	if !hasUser {
		return nil, nil
	}

	// Build the index ↔ uuid mapping. Order matches the Questions slice
	// so Q1 is questions[0], Q2 is questions[1], etc.
	idToLabel := make(map[string]string, len(params.Questions))
	labelToID := make(map[string]string, len(params.Questions))
	for i, q := range params.Questions {
		label := "Q" + strconv.Itoa(i+1)
		idToLabel[q.ID] = label
		labelToID[label] = q.ID
	}

	// Project previously-covered UUIDs into Q-labels (drop any that no
	// longer match an active question — those belong to archived rows).
	prevLabels := make([]string, 0, len(params.PreviouslyCovered))
	prevSet := make(map[string]struct{}, len(params.PreviouslyCovered))
	for _, id := range params.PreviouslyCovered {
		if label, ok := idToLabel[id]; ok {
			prevLabels = append(prevLabels, label)
			prevSet[id] = struct{}{}
		}
	}

	user := buildCoverageUserPrompt(params.Questions, params.Messages, prevLabels)
	resp, err := client.Complete(ctx, llm.CompletionRequest{
		Model:     params.Model,
		System:    chatCoverageSystemPrompt,
		User:      user,
		MaxTokens: coverageMaxTokens,
		JSONMode:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("coverage classifier llm call: %w", err)
	}
	cleaned := stripFences(resp.Content)
	var raw struct {
		NewlyCovered []string `json:"newly_covered"`
	}
	if err := json.Unmarshal([]byte(cleaned), &raw); err != nil {
		return nil, fmt.Errorf("parse coverage response: %w (content: %s)", err, truncate(resp.Content, 300))
	}

	// Resolve delta labels → UUIDs, union with previously-covered.
	out := make([]string, 0, len(prevSet)+len(raw.NewlyCovered))
	for _, id := range params.PreviouslyCovered {
		if _, ok := idToLabel[id]; ok { // keep only ids still active
			out = append(out, id)
		}
	}
	seen := make(map[string]struct{}, len(out))
	for _, id := range out {
		seen[id] = struct{}{}
	}
	for _, label := range raw.NewlyCovered {
		label = strings.TrimSpace(label)
		id, ok := labelToID[label]
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

// buildCoverageUserPrompt assembles the user-message body sent to the
// classifier. The format is intentionally compact for small models:
// Q-labelled question list, the already-covered label set as state,
// and a recent transcript window (last coverageRecentWindow user/
// assistant rows).
func buildCoverageUserPrompt(
	questions []QuestionView,
	messages []domain.ChatMessage,
	previouslyCovered []string,
) string {
	var b strings.Builder
	b.WriteString("Active questions:\n")
	for i, q := range questions {
		fmt.Fprintf(&b, "- Q%d: %s\n", i+1, q.Prompt)
	}
	b.WriteString("\nAlready covered: ")
	if len(previouslyCovered) == 0 {
		b.WriteString("(none)")
	} else {
		b.WriteString(strings.Join(previouslyCovered, ", "))
	}
	b.WriteString("\n\nRecent transcript (oldest → newest):\n")

	lines := TranscriptLinesFromMessages(messages)
	if len(lines) > coverageRecentWindow {
		lines = lines[len(lines)-coverageRecentWindow:]
	}
	for _, line := range lines {
		fmt.Fprintf(&b, "[%s] %s\n", line.Role, line.Content)
	}
	return b.String()
}
