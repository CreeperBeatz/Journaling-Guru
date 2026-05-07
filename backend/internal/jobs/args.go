// Package jobs hosts the River workers and scheduler used by the
// summary pipeline. The worker (cmd/worker) and api (cmd/api) both pull
// from this package — the api uses Scheduler.LazySeed when a journal
// entry lands; the worker drives RunSummary off the dispatcher tick.
package jobs

import "github.com/riverqueue/river"

// SummaryArgs is the River job payload. Only the summary_jobs row id is
// transported — the worker reads the rest of the state from Postgres,
// which keeps River's queue rows tiny and makes a "regenerate this
// summary" trigger as simple as "set summary_jobs.status='pending' and
// claim it next tick."
type SummaryArgs struct {
	JobID string `json:"job_id"`
}

// Kind is the River job-name constant. Stable across versions so we
// don't orphan in-flight rows on deploy.
func (SummaryArgs) Kind() string { return "summary" }

// InsertOpts pins the queue and timeout. Summaries can take 30+ seconds
// for a yearly with 12 monthlies; River default is 1 minute, but we
// nudge it up to give Claude headroom on the long-context calls.
func (SummaryArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       river.QueueDefault,
		MaxAttempts: 5,
	}
}

// EmotionClassifyArgs is the River payload for the Plutchik classifier.
// Same shape as SummaryArgs — just an emotion_classify_jobs row id; the
// worker reads the rest from Postgres.
type EmotionClassifyArgs struct {
	JobID string `json:"job_id"`
}

func (EmotionClassifyArgs) Kind() string { return "emotion_classify" }

// MaxAttempts is lower than SummaryArgs because the call is short
// (one prompt, capped at 400 tokens) — if it fails three times something
// systemic is wrong, no point hammering OpenRouter.
func (EmotionClassifyArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       river.QueueDefault,
		MaxAttempts: 3,
	}
}
