package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/chat"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// MemoryExtractionWorker runs the per-day memory reconciliation pass:
// load the day's canonical journal record (journal_entries +
// daily_inputs — already post-manual-wins), show it to the LLM next to
// the user's current memory list, and apply the validated
// ADD/UPDATE/DELETE ops.
//
// Idempotency model: terminal job statuses early-return; the apply step
// and the job's completed transition commit in ONE transaction
// (MemoryStore.ApplyExtractionOps), so a re-claimed job either sees a
// terminal row or a clean slate — never a half-applied pass. Pinned
// (user-edited) memories are excluded from the writable set in the
// prompt, dropped in op validation, AND re-checked inside the apply
// transaction (manual-wins, three layers deep).
type MemoryExtractionWorker struct {
	river.WorkerDefaults[MemoryExtractionArgs]

	Jobs        *store.MemoryExtractionJobStore
	Memories    *store.MemoryStore
	Entries     *store.EntryStore
	DailyInputs *store.DailyInputStore
	Users       *store.UserStore
	// LLM is the classify-tier client (CLASSIFY_MODEL default).
	LLM    *llm.OpenRouter
	Logger *slog.Logger
}

// Work is River's entrypoint. Mirrors the ChatExtractionWorker
// error-encoding pattern: errors persist into
// memory_extraction_jobs.last_error so a re-claim has context, and the
// final attempt marks failed instead of bubbling to River.
func (w *MemoryExtractionWorker) Work(ctx context.Context, rj *river.Job[MemoryExtractionArgs]) error {
	job, err := w.Jobs.GetByID(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, store.ErrMemoryExtractionJobNotFound) {
			// Race with delete cascade (user deleted account). Don't retry.
			return nil
		}
		return err
	}
	switch job.Status {
	case "completed", "skipped", "failed":
		// Terminal — a stale River retry can land here.
		return nil
	}

	user, err := w.Users.GetByID(ctx, job.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		// User soft-deleted between scheduling and firing.
		return w.Jobs.MarkSkipped(ctx, job.ID)
	}

	if err := w.process(ctx, job, user); err != nil {
		isFinal := rj.Attempt >= rj.MaxAttempts
		w.Logger.Warn("memory extraction error",
			"err", err,
			"job_id", job.ID,
			"user_id", job.UserID,
			"local_date", job.LocalDate,
			"attempt", rj.Attempt,
			"max", rj.MaxAttempts,
		)
		if isFinal {
			_ = w.Jobs.MarkFailed(ctx, job.ID, err.Error())
			return nil
		}
		_ = w.Jobs.ReleaseForRetry(ctx, job.ID, err.Error())
		return err
	}
	return nil
}

// process is the inner reconciliation flow, separated so Work can do the
// error-encoding wrapper without nesting.
func (w *MemoryExtractionWorker) process(
	ctx context.Context,
	job *domain.MemoryExtractionJob,
	user *domain.User,
) error {
	localDate, err := time.Parse("2006-01-02", job.LocalDate)
	if err != nil {
		return fmt.Errorf("parse local_date: %w", err)
	}

	daily, err := w.DailyInputs.GetByDate(ctx, user.ID, localDate)
	if err != nil {
		return fmt.Errorf("load daily_inputs: %w", err)
	}
	entries, err := w.Entries.ListByDateWithPrompts(ctx, user.ID, localDate)
	if err != nil {
		return fmt.Errorf("load entries: %w", err)
	}
	if daily == nil && len(entries) == 0 {
		// Nothing was written for the day after all — no LLM call.
		return w.Jobs.MarkSkipped(ctx, job.ID)
	}

	memories, err := w.Memories.ListActive(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("load memories: %w", err)
	}
	writable, pinned := chat.MemoryViewsFromDomain(memories)

	params := chat.MemoryReconcileParams{
		LocalDate: job.LocalDate,
		Weekday:   localDate.Weekday().String(),
		Writable:  writable,
		Pinned:    pinned,
	}
	if daily != nil {
		params.Daily = &chat.MemoryDayInput{
			MoodLabel:      domain.MoodLabel(daily.Mood),
			DrainedText:    daily.DrainedText,
			ChargedText:    daily.ChargedText,
			GratitudeText:  daily.GratitudeText,
			ReflectionText: daily.ReflectionText,
		}
	}
	for _, e := range entries {
		params.Entries = append(params.Entries, chat.MemoryDayEntry{
			Prompt: e.Prompt,
			Body:   e.Body,
		})
	}

	ops, err := chat.MemoryReconcile(ctx, w.LLM, params)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		// Valid outcome — most days hold no new durable facts. Terminal
		// via MarkSkipped is wrong here (the LLM did run); complete with
		// zero ops through the same atomic path.
		w.Logger.Info("memory reconcile: no ops",
			"user_id", user.ID, "local_date", job.LocalDate)
	}

	if err := w.Memories.ApplyExtractionOps(ctx, user.ID, localDate, ops, job.ID); err != nil {
		return fmt.Errorf("apply memory ops: %w", err)
	}
	w.Logger.Info("memory reconcile applied",
		"user_id", user.ID, "local_date", job.LocalDate, "ops", len(ops))
	return nil
}
