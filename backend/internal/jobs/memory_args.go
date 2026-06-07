package jobs

import "github.com/riverqueue/river"

// MemoryExtractionArgs is the River payload for the per-day memory
// reconciliation pass. Only the memory_extraction_jobs row id is
// transported; the worker reads the rest (day content, memory list)
// from Postgres.
//
// Same shape pattern as SummaryArgs / ChatExtractionArgs so the
// dispatcher tick can drain all five queues identically.
type MemoryExtractionArgs struct {
	JobID string `json:"job_id"`
}

// Kind is the River job-name constant. Stable across versions so we
// don't orphan in-flight rows on deploy.
func (MemoryExtractionArgs) Kind() string { return "memory_extraction" }

// InsertOpts: 4 attempts, same rationale as chat extraction — one
// classify-tier call whose dominant failure mode is JSON parse, plus
// transient OpenRouter blips.
func (MemoryExtractionArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       river.QueueDefault,
		MaxAttempts: 4,
	}
}
