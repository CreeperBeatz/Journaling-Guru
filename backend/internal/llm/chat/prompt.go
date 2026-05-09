package chat

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/prompts"
)

// chatPersonaPrompt is the static prefix of the system prompt — same on
// every turn of every session. Goes into the prompt-cache window so
// turn 2+ pays only for the dynamic suffix.
//
// Section order is deliberate:
//   1. role identity (anchors voice)
//   2. priority order (engagement → questions → reflection — the user's
//      explicit ranking; this is the load-bearing steer)
//   3. how-you-talk rules
//   4. hard rules (safety + structural)
//   5. tool affordances (advisory only)
//
// The questions list, recent context, and phase block are NOT here —
// they live in daily_chat_context.tmpl so they don't bust the cache
// every turn.
const chatPersonaPrompt = `You are Journaling Guru's reflective journal companion. You help one person —
the user — reflect on their day through warm, plain-spoken conversation,
focused on the Energy Audit: what drained, what charged, what they're
grateful for, and anything else on their mind.

# Priorities (in order)

1. **Engagement.** Make it feel like a real conversation. Warmth and
   presence first. A session that covers one topic honestly is better
   than one that touches all four mechanically.
2. **The four topics.** Drained / charged / grateful / anything-else —
   weave them in organically. Cover what naturally fits; don't march
   through them. A "nothing today" answer is valid for any.
3. **Reflect on the day.** Beyond the four, surface mood and tone. The
   extraction step at session-end captures all of this — your job is
   to invite it, not catalog it.

# How you talk

- Each reply: ONE message, 40-80 tokens. Cut yourself off — the user
  fills the silence. Long replies kill the rhythm.
- ONE open question per turn. Never two. Never a list.
- Reflect what you heard in one short sentence, then ask. Don't
  summarize the user back to themselves verbatim — pick the pivot.
- Use second person ("you noticed..."), not first person plural ("we").
- No sycophancy filler. Skip "That's so valid.", "I'm glad you shared
  that.", "What a beautiful insight.". Just respond.
- No clinical or therapeutic framing. You are not a therapist. Don't
  say "How does that make you feel?" — too well-known to land.
- Plain language. Match their register; if they're casual, be casual.

# Hard rules

- You are not a clinician. Never give medical, psychiatric, or
  pharmacological advice. If the user mentions self-harm, suicide, an
  active crisis, or asks for clinical help: respond with care for
  exactly two sentences and stop. The system handles crisis resources
  separately.
- Never invent facts about the user. No "I remember when you said X"
  unless X is verbatim earlier in this transcript.
- Keep replies bounded to 40-80 tokens. Cut the second clause if it's
  not load-bearing.
- The session has a soft budget of 5-15 minutes (and a 30-minute hard
  cap the system enforces). Don't reference the timer.

# Tools (call sparingly, never announce)

- propose_wrap_up({}) — when the user signals winding down ("alright,
  I think that's it", "okay I should sleep", or visibly disengaging
  short replies after a real arc). Surfaces a wrap-up affordance to
  the user; they decide whether to actually end.

Coverage of the four topics is tracked separately by a post-turn
classifier — you do NOT need to mark anything; just converse, and the
system measures coverage from the transcript.

The user's session-specific context (recent mood, phase) follows below.`

// chatExtractionSystemPrompt drives the single-shot extraction LLM call
// at session end. JSON-mode + per-call model override; default model
// is the classify-tier client (CLASSIFY_MODEL), with per-session pin on
// chat_sessions.extraction_model winning when set.
//
// Anti-hallucination is the priority: the extractor must omit keys it
// can't substantiate, never invent.
const chatExtractionSystemPrompt = `You extract structured journal data from a reflective conversation
transcript into the Energy Audit's five-prompt template. You are not a
writer — you find what the user said and sort it into slots.

Emit ONE JSON object — no prose before/after, no markdown fences. The
schema is exactly:

{
  "mood":                  <integer 1..3 OR null>,  // 1=sad, 2=neutral, 3=happy
  "drained_text":          <string>,                 // ≤ 1000 chars
  "charged_text":          <string>,                 // ≤ 1000 chars
  "gratitude_text":        <string>,                 // ≤ 1000 chars
  "reflection_text":       <string>,                 // ≤ 4000 chars
  "drained_tag_proposals": [<short label>],          // ≤ 5, lowercase
  "charged_tag_proposals": [<short label>],          // ≤ 5, lowercase
  "answers":               {<question_id>: <string>} // omit uncovered keys
}

# Rules

- mood: ONLY set when the user clearly signaled how the day felt
  ("kind of a great day", "rough today"). 1=clearly negative,
  2=mixed/flat, 3=clearly positive. Default null when ambiguous — do
  NOT infer from tone alone.
- drained_text: what drained the user today, in the user's words. ≤
  1000 chars. Empty string if the conversation didn't surface anything.
- charged_text: what charged the user today, in the user's words. Same
  bounds.
- gratitude_text: what the user named as something they're grateful
  for. Same bounds. NOT analyzed; copy verbatim.
- reflection_text: anything else the user shared that didn't fit one
  of the four slots. ≤ 4000 chars. Empty string if nothing fits.
- drained_tag_proposals: short, reusable labels for what drained the
  user — 1–4 words each, lowercase, in the user's idiom (e.g. "back-
  to-back meetings", "poor sleep", "social media"). ≤ 5 items. Empty
  array if drained_text is empty or the drainer is too one-off to be
  a recurring tag (e.g. "the dentist on Tuesday" — not a tag).
- charged_tag_proposals: same shape, for chargers (e.g. "morning walk",
  "deep work", "exercise"). ≤ 5 items.
- answers: only include keys for questions the user substantively
  answered. NEVER invent or guess. The user's voice — third-person
  paragraphs are fine, but no meta-commentary, no questions.
- Never quote the assistant's words. Never quote tool calls.
- The transcript is verbatim. Use the user's voice, not yours.`

// QuestionView is the minimal shape passed into the prompt templates.
// Decoupled from domain.Question so the templates don't carry archive
// timestamps or position fields.
type QuestionView struct {
	ID     string
	Prompt string
}

// QuestionViewsFromDomain converts active questions to the prompt
// view shape, dropping archived rows.
func QuestionViewsFromDomain(qs []domain.Question) []QuestionView {
	out := make([]QuestionView, 0, len(qs))
	for _, q := range qs {
		if q.ArchivedAt != nil {
			continue
		}
		out = append(out, QuestionView{ID: q.ID, Prompt: q.Prompt})
	}
	return out
}

// BuildSystemPromptParams is the shape passed into BuildSystemPrompt.
// Recent7DayMoodAvg and RecentTopEmotions can be nil/empty — the
// template renders sensibly either way.
type BuildSystemPromptParams struct {
	DisplayName       string
	LocalDate         string
	Weekday           string
	LocalTimeOfDay    string
	Questions         []QuestionView
	Recent7DayMoodAvg *float64
	RecentTopEmotions []string
	Phase             string
	HardCapMinutes    int
}

// BuildSystemPrompt concatenates the static persona prompt with the
// rendered dynamic context block. The static prefix is cache-friendly;
// keep it stable.
func BuildSystemPrompt(p BuildSystemPromptParams) (string, error) {
	ctx, err := renderChatTemplate("daily_chat_context.tmpl", p)
	if err != nil {
		return "", err
	}
	return chatPersonaPrompt + "\n\n" + ctx, nil
}

// TranscriptLine is a row in the extraction prompt transcript view.
// Role is rendered as-is; the extraction prompt keeps user/assistant
// distinction so the model knows whose words to extract from.
type TranscriptLine struct {
	Role    string
	Content string
}

// TranscriptLinesFromMessages converts persisted messages into the
// extraction prompt's view shape. tool / system_event rows are
// dropped — the extractor only consumes user/assistant turns.
func TranscriptLinesFromMessages(messages []domain.ChatMessage) []TranscriptLine {
	out := make([]TranscriptLine, 0, len(messages))
	for _, m := range messages {
		if m.Role != domain.ChatRoleUser && m.Role != domain.ChatRoleAssistant {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		out = append(out, TranscriptLine{Role: m.Role, Content: content})
	}
	return out
}

// BuildExtractionPrompts returns (system, user) prompts for the
// extraction step. System is a constant; user is rendered from the
// transcript template.
func BuildExtractionPrompts(questions []QuestionView, messages []domain.ChatMessage) (string, string, error) {
	user, err := renderChatTemplate("daily_chat_extract.tmpl", map[string]any{
		"Questions": questions,
		"Messages":  TranscriptLinesFromMessages(messages),
	})
	if err != nil {
		return "", "", err
	}
	return chatExtractionSystemPrompt, user, nil
}

// MessagesForLLM converts persisted ChatMessage rows into the OpenAI-
// compatible Message shape for the streaming call. tool / system_event
// rows are surfaced as compact context strings so the model can stay
// aware of out-of-band events (e.g. "user dismissed crisis card") but
// they don't get a real assistant turn.
func MessagesForLLM(messages []domain.ChatMessage) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case domain.ChatRoleUser:
			out = append(out, llm.Message{Role: "user", Content: m.Content})
		case domain.ChatRoleAssistant:
			content := m.Content
			if m.ToolName != nil && *m.ToolName != "" && content == "" {
				// Pure tool-call assistant turn; surface as a system
				// note so the model sees it without hallucinating
				// content.
				content = fmt.Sprintf("(internal: previously called tool %s)", *m.ToolName)
			}
			out = append(out, llm.Message{Role: "assistant", Content: content})
		case domain.ChatRoleSystemEvent:
			// Inject as a system note in-line. Cheap; helps the model
			// stay coherent across out-of-band events.
			out = append(out, llm.Message{Role: "system", Content: "(event: " + m.Content + ")"})
		}
		// tool rows are intentionally skipped — they're persisted for
		// audit but are not part of the model-visible transcript.
	}
	return out
}

// TimeOfDay maps a wall-clock hour (0-23) to a coarse label used in
// the system prompt. Boundaries are deliberately fuzzy — "evening"
// covers 17-21 because most journaling happens after dinner.
func TimeOfDay(t time.Time) string {
	h := t.Hour()
	switch {
	case h < 5:
		return "late night"
	case h < 12:
		return "morning"
	case h < 17:
		return "afternoon"
	case h < 22:
		return "evening"
	default:
		return "late night"
	}
}

// renderChatTemplate executes one of the embedded chat templates with
// the shared funcMap. Mirrors jobs/prompts.go::renderTemplate but is
// scoped to this package so we don't introduce a cross-package dep.
func renderChatTemplate(name string, data any) (string, error) {
	raw, err := prompts.FS.ReadFile(name)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", name, err)
	}
	t, err := template.New(name).Funcs(template.FuncMap{
		"joinStrings": strings.Join,
	}).Parse(string(raw))
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute %s: %w", name, err)
	}
	return strings.TrimSpace(buf.String()), nil
}
