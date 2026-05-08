package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
)

// Coverage codes for the Energy Audit's four fixed topics. The classifier
// emits these strings verbatim; anything not in this set is dropped
// post-parse. Values are persisted in chat_sessions.covered_question_ids
// (column name retained from the per-question era — under the pivot it
// stores topic codes, not UUIDs).
const (
	CoverageCodeDrained  = "drained"
	CoverageCodeCharged  = "charged"
	CoverageCodeGrateful = "grateful"
	CoverageCodeElse     = "else"
)

// CoverageCodes is the canonical ordered list rendered as chips in the
// FE. Stable order: spec's prompt order (drained → charged → grateful →
// else).
var CoverageCodes = []string{
	CoverageCodeDrained,
	CoverageCodeCharged,
	CoverageCodeGrateful,
	CoverageCodeElse,
}

func validCoverageCode(s string) bool {
	switch s {
	case CoverageCodeDrained, CoverageCodeCharged, CoverageCodeGrateful, CoverageCodeElse:
		return true
	}
	return false
}

// coverageMaxTokens caps the classifier response. Output is a short
// list of topic codes; 80 tokens is plenty and leaves the model no
// room to ramble. Latency-sensitive: this fires after every assistant
// turn.
const coverageMaxTokens = 80

// coverageRecentWindow is how many trailing chat_messages rows the
// classifier sees. Coverage is judged in delta mode against the
// previously-covered set, so the model only needs the latest few
// turns of context to decide what to add.
const coverageRecentWindow = 8

// chatCoverageSystemPrompt drives the post-turn classifier under the
// Energy Audit pivot. JSON-mode + the classify-tier client. Anti-
// hallucination is the priority: unknown codes are dropped post-parse.
const chatCoverageSystemPrompt = `You watch a reflective journal conversation and decide which of the
four fixed Energy Audit topics have JUST become substantively addressed
in the latest turn(s).

The four topics are:
  - "drained":   what drained the user today (negative)
  - "charged":   what charged the user today (positive)
  - "grateful":  what they're grateful for
  - "else":      anything else on their mind that doesn't fit the above

You will see:
- The set of topic codes ALREADY judged covered (state from prior turns).
- The most recent slice of the conversation (oldest → newest).

Output ONE JSON object — no prose before/after, no markdown fences:

{
  "newly_covered": ["drained", ...]
}

# Rules

- A topic is "covered" only when the user has shared something real and
  concrete about it. Passing mentions, deflections, or the assistant's
  prompts on the topic do NOT count.
- "Nothing today" is a valid covered answer — if the user says
  "nothing drained me", that counts as "drained" covered.
- Be CONSERVATIVE. If you're not sure, omit. A false negative is
  gentler than a false positive.
- Output ONLY codes that are NOT already in "already covered" — that's
  the delta. Never repeat codes the system already knows about.
- Use ONLY the four codes above. Never invent codes.
- Empty list ([]) is a perfectly valid answer when nothing new landed.`

// CoverageParams bundles inputs to Classify.
//
// PreviouslyCovered is the union persisted on chat_sessions — already
// stored as topic codes under the pivot.
type CoverageParams struct {
	Model             string // per-call override; empty falls back to client default
	Messages          []domain.ChatMessage
	PreviouslyCovered []string // topic codes already covered
}

// Classify runs the post-turn classifier in delta mode and returns the
// FULL covered set (previous union ∪ delta) as topic codes. Unknown
// labels emitted by the model are dropped silently.
//
// Returns (nil, nil) when there's nothing usable to classify (no user
// turns yet — typical for the opener path).
func Classify(
	ctx context.Context,
	client *llm.OpenRouter,
	params CoverageParams,
) ([]string, error) {
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

	// Filter previous to known codes (drops stale data from the
	// per-question era, where the column stored UUIDs).
	prev := make([]string, 0, len(params.PreviouslyCovered))
	prevSet := make(map[string]struct{}, len(params.PreviouslyCovered))
	for _, code := range params.PreviouslyCovered {
		if !validCoverageCode(code) {
			continue
		}
		if _, dup := prevSet[code]; dup {
			continue
		}
		prevSet[code] = struct{}{}
		prev = append(prev, code)
	}

	user := buildCoverageUserPrompt(params.Messages, prev)
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

	out := make([]string, 0, len(prev)+len(raw.NewlyCovered))
	out = append(out, prev...)
	for _, code := range raw.NewlyCovered {
		code = strings.ToLower(strings.TrimSpace(code))
		if !validCoverageCode(code) {
			continue
		}
		if _, dup := prevSet[code]; dup {
			continue
		}
		prevSet[code] = struct{}{}
		out = append(out, code)
	}
	return out, nil
}

// buildCoverageUserPrompt assembles the user-message body sent to the
// classifier. Compact format for small models: the already-covered
// code set as state, plus a recent transcript window.
func buildCoverageUserPrompt(
	messages []domain.ChatMessage,
	previouslyCovered []string,
) string {
	var b strings.Builder
	b.WriteString("Already covered: ")
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
