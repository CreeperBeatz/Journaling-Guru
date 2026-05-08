package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// Per-period output token caps. Conservative — we'd rather see a clipped
// reflection than a hallucinated next-paragraph.
const weeklyMaxTokens = 800

// SummaryWorker handles SummaryArgs jobs. Under the Energy Audit pivot
// only the weekly period survives; the daily / monthly / yearly LLM
// paths are retired (CHECK constraint on summary_jobs.period_type
// enforces this at the DB layer too).
type SummaryWorker struct {
	river.WorkerDefaults[SummaryArgs]

	Summaries   *store.SummaryStore
	Jobs        *store.SummaryJobStore
	Entries     *store.EntryStore
	DailyInputs *store.DailyInputStore
	Users       *store.UserStore
	Scheduler   *Scheduler
	LLM         *llm.OpenRouter
	Logger      *slog.Logger
}

// Work is River's entrypoint. We catch errors at this boundary so we
// can encode them into summary_jobs.status; the returned error is what
// drives River's retry decision.
func (w *SummaryWorker) Work(ctx context.Context, rj *river.Job[SummaryArgs]) error {
	job, err := w.Jobs.GetByID(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, store.ErrSummaryJobNotFound) {
			// Race with a delete cascade (user deleted account). Don't retry.
			return nil
		}
		return err
	}
	switch job.Status {
	case "completed", "skipped", "failed", "cancelled":
		return nil
	}

	user, err := w.Users.GetByID(ctx, job.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return w.Jobs.MarkSkipped(ctx, job.ID)
	}

	if err := w.process(ctx, job, user); err != nil {
		isFinal := rj.Attempt >= rj.MaxAttempts
		w.Logger.Warn("summary worker error",
			"err", err,
			"job_id", job.ID,
			"period", job.PeriodType,
			"period_start", job.PeriodStart,
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

func (w *SummaryWorker) process(ctx context.Context, job *domain.SummaryJob, user *domain.User) error {
	periodStart, err := time.Parse("2006-01-02", job.PeriodStart)
	if err != nil {
		return fmt.Errorf("parse period_start: %w", err)
	}
	period, err := timezone.PeriodFromLocalStart(
		periodStart, user.Timezone, user.DayStartMinutes,
		domain.SummaryPeriod(job.PeriodType),
	)
	if err != nil {
		return fmt.Errorf("recompute period: %w", err)
	}
	if domain.SummaryPeriod(job.PeriodType) != domain.PeriodWeek {
		// Defensive: post-pivot the DB CHECK only allows 'week', but if
		// a stale row sneaks through (e.g. a paused job from before the
		// constraint tightened), skip it instead of failing.
		w.Logger.Info("skipping retired period type",
			"job_id", job.ID, "period", job.PeriodType)
		return w.skip(ctx, job)
	}
	return w.runWeekly(ctx, job, user, period)
}

func (w *SummaryWorker) runWeekly(
	ctx context.Context, job *domain.SummaryJob, user *domain.User, period timezone.Period,
) error {
	hasContent, err := w.DailyInputs.HasContentInRange(ctx, user.ID, period.Start, period.End)
	if err != nil {
		return fmt.Errorf("check content: %w", err)
	}
	if !hasContent {
		return w.skip(ctx, job)
	}

	agg, err := w.DailyInputs.AggregateForRange(ctx, user.ID, period.Start, period.End)
	if err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}

	// Phase-1 stub: render the weekly template with whatever data the
	// aggregate provides. Phase 6 swaps this for a tag-table-driven
	// headline insight prompt.
	userPrompt, err := renderTemplate("weekly.tmpl", map[string]any{
		"PeriodStart":    period.Start.Format("2006-01-02"),
		"PeriodEnd":      period.End.Format("2006-01-02"),
		"DailySummaries": []weeklyDailyChild{}, // empty until Phase 6 rewrite
	})
	if err != nil {
		return err
	}
	resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
		System:    weeklySystemPrompt,
		User:      userPrompt,
		MaxTokens: weeklyMaxTokens,
	})
	if err != nil {
		return err
	}

	meta := domain.SummaryMetadata{
		EntryCount: agg.EntryCount,
		MoodScore:  agg.MoodScore,
	}
	if agg.MoodScore != nil {
		rounded := int(*agg.MoodScore + 0.5)
		meta.MoodLabel = domain.MoodLabel(&rounded)
	}

	if _, err := w.Summaries.Upsert(
		ctx, user.ID, string(domain.PeriodWeek),
		period.Start, period.End,
		strings.TrimSpace(resp.Content), meta,
		resp.Model, resp.PromptTokens, resp.CompletionTokens,
	); err != nil {
		return fmt.Errorf("upsert summary: %w", err)
	}
	return w.complete(ctx, job)
}

func (w *SummaryWorker) complete(ctx context.Context, job *domain.SummaryJob) error {
	if err := w.Jobs.MarkCompleted(ctx, job.ID); err != nil {
		return err
	}
	if err := w.Scheduler.ScheduleNext(ctx, job); err != nil {
		w.Logger.Warn("schedule next failed", "err", err, "job_id", job.ID)
	}
	return nil
}

func (w *SummaryWorker) skip(ctx context.Context, job *domain.SummaryJob) error {
	if err := w.Jobs.MarkSkipped(ctx, job.ID); err != nil {
		return err
	}
	if err := w.Scheduler.ScheduleNext(ctx, job); err != nil {
		w.Logger.Warn("schedule next failed (skipped path)", "err", err, "job_id", job.ID)
	}
	return nil
}

