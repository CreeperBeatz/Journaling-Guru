package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
)

// memoryReconcileSystemPrompt drives the per-day memory pass: given the
// day's canonical journal record and the user's existing memory list,
// emit Mem0-style ADD/UPDATE/DELETE operations. Durability is the core
// rule — the biggest failure mode is minting transient daily states as
// permanent facts.
const memoryReconcileSystemPrompt = `You maintain a small list of durable facts about one journaling user — their "memory". Each day you receive their journal record for that day plus the current memory list, and you reconcile the two.

Respond with ONLY a JSON object, no prose, no markdown fences:

{
  "operations": [
    {"op": "add", "category": "<category>", "content": "<one sentence, ≤300 chars>"},
    {"op": "update", "id": "<id from the writable list>", "category": "<category>", "content": "<replacement sentence>"},
    {"op": "delete", "id": "<id from the writable list>"}
  ]
}

Categories (use exactly one of): identity, relationships, work, health, preferences, goals, routines, other.

What qualifies as a memory — the durability test:
A memory must be a fact that will still be true and useful weeks from now. Ask: "would a thoughtful friend still know this next month?"

GOOD (durable):
- "Has a sister named Mara who lives nearby" (relationships)
- "Works as an ICU nurse on rotating night shifts" (work)
- "Started a new job at Acme in June 2026" (work)
- "Training for a half-marathon in October" (goals)
- "Long commutes reliably drain them" (preferences)
- "Practices guitar most evenings to decompress" (routines)

BAD (transient — never store):
- "Felt tired today" / "Had a stressful meeting" — daily states
- "Is grateful for the sunny weather" — momentary
- "Mood was happy" — that's the daily check-in, not a memory
- Anything the user only speculated about ("maybe I should…")

Rules:
1. Only state facts the day's record actually supports. Never infer beyond what was written.
2. Prefer UPDATE over ADD when a writable memory covers the SAME underlying fact and today's record refines or changes it ("started the new job" → update the old job fact; do not keep both). An update must stay about the same fact — NEVER repurpose an unrelated memory's id to record something new; that destroys the old memory. New fact → ADD.
3. Emit DELETE only when today's record directly contradicts a memory and no replacement fact fits.
4. Never reference an id that is not in the writable list. User-locked memories are read-only AND authoritative — never re-add a fact they cover, and never ADD a fact that contradicts or revises one. If today's record conflicts with a locked memory, leave it alone entirely; the user controls locked facts.
5. Write each content as one self-contained sentence in third person, no "the user" prefix needed — start with the verb phrase ("Has…", "Works…", "Prefers…").
6. Most days contain zero or one new durable fact. Emit at most 8 operations. An empty operations array is a perfectly good answer.`

// memoryReconcileMaxTokens caps the response — ops are small; 8 ops at
// ~80 tokens each plus envelope fits comfortably.
const memoryReconcileMaxTokens = 1200

// Bounds enforced post-parse. Content cap matches the user_memories
// CHECK (≤500); ops cap is defensive headroom over the prompted 8.
const (
	memoryMaxContentChars = 500
	memoryMaxOps          = 12
)

// MemoryView is the minimal memory shape passed into the reconcile
// template. Decoupled from domain.Memory so the template carries no
// status/pinned internals.
type MemoryView struct {
	ID       string
	Category string
	Content  string
}

// MemoryDayInput mirrors the daily check-in fields the template renders.
type MemoryDayInput struct {
	MoodLabel      string
	DrainedText    string
	ChargedText    string
	GratitudeText  string
	ReflectionText string
}

// MemoryDayEntry is one (question, answer) pair from the day's journal.
type MemoryDayEntry struct {
	Prompt string
	Body   string
}

// MemoryReconcileParams bundles the inputs to MemoryReconcile.
type MemoryReconcileParams struct {
	Model     string // per-call override; empty falls back to client default
	LocalDate string // YYYY-MM-DD
	Weekday   string
	Daily     *MemoryDayInput // nil when no check-in exists
	Entries   []MemoryDayEntry
	Writable  []MemoryView // active, non-pinned — eligible for update/delete
	Pinned    []MemoryView // active, pinned — read-only context
}

// MemoryReconcile runs the reconciliation LLM call and returns validated
// ops, safe to feed into MemoryStore.ApplyExtractionOps. Update/delete
// ops are filtered against the writable set — an op targeting a pinned
// or hallucinated id is dropped here (and the store re-checks inside the
// apply transaction).
func MemoryReconcile(
	ctx context.Context,
	client *llm.OpenRouter,
	params MemoryReconcileParams,
) ([]domain.MemoryOp, error) {
	user, err := renderChatTemplate("memory_reconcile.tmpl", params)
	if err != nil {
		return nil, err
	}
	resp, err := client.Complete(ctx, llm.CompletionRequest{
		Model:     params.Model,
		System:    memoryReconcileSystemPrompt,
		User:      user,
		MaxTokens: memoryReconcileMaxTokens,
		JSONMode:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("memory reconcile llm call: %w", err)
	}
	var parsed struct {
		Operations []struct {
			Op       string `json:"op"`
			ID       string `json:"id"`
			Category string `json:"category"`
			Content  string `json:"content"`
		} `json:"operations"`
	}
	if err := json.Unmarshal([]byte(stripFences(resp.Content)), &parsed); err != nil {
		return nil, fmt.Errorf("parse memory reconcile response: %w (content: %s)", err, truncate(resp.Content, 300))
	}

	writableIDs := make(map[string]struct{}, len(params.Writable))
	for _, m := range params.Writable {
		writableIDs[m.ID] = struct{}{}
	}

	out := make([]domain.MemoryOp, 0, len(parsed.Operations))
	for _, raw := range parsed.Operations {
		if len(out) >= memoryMaxOps {
			break
		}
		content := clampText(raw.Content, memoryMaxContentChars)
		category := raw.Category
		if !domain.IsValidMemoryCategory(category) {
			category = "other"
		}
		switch raw.Op {
		case domain.MemoryOpAdd:
			if content == "" {
				continue
			}
			out = append(out, domain.MemoryOp{
				Op: domain.MemoryOpAdd, Category: category, Content: content,
			})
		case domain.MemoryOpUpdate:
			if content == "" {
				continue
			}
			if _, ok := writableIDs[raw.ID]; !ok {
				continue // pinned or hallucinated id
			}
			out = append(out, domain.MemoryOp{
				Op: domain.MemoryOpUpdate, ID: raw.ID, Category: category, Content: content,
			})
		case domain.MemoryOpDelete:
			if _, ok := writableIDs[raw.ID]; !ok {
				continue
			}
			out = append(out, domain.MemoryOp{Op: domain.MemoryOpDelete, ID: raw.ID})
		}
	}
	return out, nil
}

// MemoryViewsFromDomain splits active memories into (writable, pinned)
// views for MemoryReconcileParams.
func MemoryViewsFromDomain(memories []domain.Memory) (writable, pinned []MemoryView) {
	for _, m := range memories {
		v := MemoryView{ID: m.ID, Category: m.Category, Content: m.Content}
		if m.Pinned {
			pinned = append(pinned, v)
		} else {
			writable = append(writable, v)
		}
	}
	return writable, pinned
}
