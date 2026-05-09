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

	Summaries      *store.SummaryStore
	Jobs           *store.SummaryJobStore
	Entries        *store.EntryStore
	DailyInputs    *store.DailyInputStore
	DailyEntryTags *store.DailyEntryTagStore
	Users          *store.UserStore
	Scheduler      *Scheduler
	LLM            *llm.OpenRouter
	Logger         *slog.Logger
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

	// Build the tag-table inputs the headline prompt feeds on. Window
	// is one week (period.Start..period.End inclusive). 0 limit pulls
	// the full set so the prompt sees the actual top by appearance.
	weekDays := int(period.End.Sub(period.Start).Hours()/24) + 1
	if weekDays < 1 {
		weekDays = 7
	}
	var drainers, chargers []store.TagAggregate
	if w.DailyEntryTags != nil {
		drainers, _ = w.DailyEntryTags.TopByValence(ctx, user.ID, domain.TagRoleDrainer, weekDays, 5)
		chargers, _ = w.DailyEntryTags.TopByValence(ctx, user.ID, domain.TagRoleCharger, weekDays, 5)
	}

	moodSeries, _ := w.DailyInputs.MoodSeries(ctx, user.ID, weekDays)

	userPrompt := buildWeeklyHeadlinePrompt(period, agg, drainers, chargers, moodSeries)
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

// buildWeeklyHeadlinePrompt assembles the user-message body for the
// weekly headline LLM call. The model is asked to write ONE sentence
// summarizing the most informative pattern. Compact format keeps the
// prompt cheap (small input, ~30-token output).
func buildWeeklyHeadlinePrompt(
	period timezone.Period,
	agg *store.AggregatedMetadata,
	drainers, chargers []store.TagAggregate,
	moodSeries []store.DailyMoodPoint,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Week of %s to %s.\n\n",
		period.Start.Format("2006-01-02"), period.End.Format("2006-01-02"))

	if agg != nil && agg.MoodScore != nil {
		fmt.Fprintf(&b, "Average mood (1-3 scale, 1=sad 2=neutral 3=happy): %.2f over %d logged day(s).\n\n",
			*agg.MoodScore, agg.EntryCount)
	} else {
		b.WriteString("Average mood: not enough data this week.\n\n")
	}

	if len(moodSeries) > 0 {
		b.WriteString("Mood by day:\n")
		for _, p := range moodSeries {
			fmt.Fprintf(&b, "  - %s: %.0f\n", p.LocalDate, p.Score)
		}
		b.WriteString("\n")
	}

	b.WriteString("Top drainers (label, days appeared, avg mood on those days):\n")
	if len(drainers) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, d := range drainers {
			moodStr := "—"
			if d.AvgMood != nil {
				moodStr = strings.TrimRight(strings.TrimRight(strconvFloat(*d.AvgMood, 1), "0"), ".")
			}
			fmt.Fprintf(&b, "  - %s (%d day%s, mood %s)\n", d.Label, d.Appearances, plural3(d.Appearances), moodStr)
		}
	}
	b.WriteString("\nTop chargers (same shape):\n")
	if len(chargers) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, c := range chargers {
			moodStr := "—"
			if c.AvgMood != nil {
				moodStr = strings.TrimRight(strings.TrimRight(strconvFloat(*c.AvgMood, 1), "0"), ".")
			}
			fmt.Fprintf(&b, "  - %s (%d day%s, mood %s)\n", c.Label, c.Appearances, plural3(c.Appearances), moodStr)
		}
	}

	b.WriteString(
		"\nWrite ONE sentence — the most informative pattern from the table " +
			"above. If nothing stands out, say so honestly (e.g. \"a quiet, " +
			"middle-of-the-road week\"). No headers, no preamble.")
	return b.String()
}

func strconvFloat(f float64, digits int) string {
	// Tiny indirection so we don't import strconv just for this; jobs
	// pkg doesn't currently depend on strconv. fmt.Sprintf works fine.
	return fmt.Sprintf("%.*f", digits, f)
}
func plural3(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
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

