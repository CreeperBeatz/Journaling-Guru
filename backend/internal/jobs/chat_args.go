package jobs

import "github.com/riverqueue/river"

// ChatExtractionArgs is the River payload for the chat-mode extraction
// step. Only the chat_extraction_jobs row id is transported; the worker
// reads the rest of the state (session, transcript) from Postgres.
//
// Same shape pattern as SummaryArgs / EmotionClassifyArgs so the
// dispatcher tick can drain all four queues identically.
type ChatExtractionArgs struct {
	JobID string `json:"job_id"`
}

// Kind is the River job-name constant. Stable across versions so we
// don't orphan in-flight rows on deploy.
func (ChatExtractionArgs) Kind() string { return "chat_extraction" }

// InsertOpts: 4 attempts is a balance — extraction is fast (one Haiku-
// or Gemma-class call against a short transcript) but JSON-mode parsing
// is the most common failure mode and a deterministic retry seldom
// helps unless we've also rolled the model back. Three retries plus
// the initial attempt covers transient OpenRouter blips.
func (ChatExtractionArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       river.QueueDefault,
		MaxAttempts: 4,
	}
}
