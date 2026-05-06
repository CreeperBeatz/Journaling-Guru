package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// Per-period output token caps. Conservative — we'd rather see a clipped
// reflection than a hallucinated next-paragraph. Numbers are tuned to the
// target word counts in the system prompts (1 token ~= 0.75 words for
// English).
const (
	dailyMaxTokens   = 800
	weeklyMaxTokens  = 800
	monthlyMaxTokens = 1200
	yearlyMaxTokens  = 1800
)

// SummaryWorker handles SummaryArgs jobs. The dispatcher in cmd/worker
// hands one River job per due summary_jobs row; this worker dispatches
// to per-period methods, calls OpenRouter, writes the row, and schedules
// the next period.
type SummaryWorker struct {
	river.WorkerDefaults[SummaryArgs]

	Summaries *store.SummaryStore
	Jobs      *store.SummaryJobStore
	Entries   *store.EntryStore
	Users     *store.UserStore
	Scheduler *Scheduler
	LLM       *llm.OpenRouter
	Logger    *slog.Logger
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
	// 'completed' / 'skipped' / 'failed' are terminal — a stale River
	// retry can land here after our human regenerate path moved the row.
	switch job.Status {
	case "completed", "skipped", "failed":
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
		w.Logger.Warn("summary worker error",
			"err", err,
			"job_id", job.ID,
			"period", job.PeriodType,
			"period_start", job.PeriodStart,
			"attempt", rj.Attempt,
			"max", rj.MaxAttempts,
		)
		if isFinal {
			// Last attempt — mark failed and don't bubble to River
			// (returning nil prevents the river job from going to
			// 'discarded' state with no journai-side record).
			_ = w.Jobs.MarkFailed(ctx, job.ID, err.Error())
			return nil
		}
		// Release the row so the dispatcher will re-claim it next tick.
		// River will also retry; either path picks it up.
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
	// PeriodFromLocalStart, NOT PeriodContaining: the stored date is
	// already canonical, and re-running LocalDate would re-apply the
	// day_start shift and move the period back by one calendar day.
	period, err := timezone.PeriodFromLocalStart(
		periodStart, user.Timezone, user.DayStartMinutes,
		domain.SummaryPeriod(job.PeriodType),
	)
	if err != nil {
		return fmt.Errorf("recompute period: %w", err)
	}
	switch domain.SummaryPeriod(job.PeriodType) {
	case domain.PeriodDay:
		return w.runDaily(ctx, job, user, period)
	case domain.PeriodWeek:
		return w.runWeekly(ctx, job, user, period)
	case domain.PeriodMonth:
		return w.runMonthly(ctx, job, user, period)
	case domain.PeriodYear:
		return w.runYearly(ctx, job, user, period)
	}
	return fmt.Errorf("unknown period type %q", job.PeriodType)
}

// ---------------- Daily ----------------

type dailyTemplateData struct {
	Date    string
	Entries []dailyEntry
}

type dailyLLMResponse struct {
	Body      string   `json:"body"`
	Emotions  []string `json:"emotions"`
	MoodScore *float64 `json:"mood_score"`
	MoodLabel string   `json:"mood_label"`
	Topics    []string `json:"topics"`
}

func (w *SummaryWorker) runDaily(
	ctx context.Context, job *domain.SummaryJob, user *domain.User, period timezone.Period,
) error {
	rows, err := w.Entries.ListByDateWithPrompts(ctx, user.ID, period.Start)
	if err != nil {
		return fmt.Errorf("load entries: %w", err)
	}
	if len(rows) == 0 {
		return w.skip(ctx, job)
	}

	entries := make([]dailyEntry, 0, len(rows))
	for _, r := range rows {
		entries = append(entries, dailyEntry{Prompt: r.Prompt, Body: r.Body})
	}
	userPrompt, err := renderTemplate("daily.tmpl", dailyTemplateData{
		Date:    period.Start.Format("2006-01-02"),
		Entries: entries,
	})
	if err != nil {
		return err
	}
	resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
		System:    dailySystemPrompt,
		User:      userPrompt,
		MaxTokens: dailyMaxTokens,
		JSONMode:  true,
	})
	if err != nil {
		return err
	}
	parsed, err := parseDailyJSON(resp.Content)
	if err != nil {
		return fmt.Errorf("parse daily LLM response: %w (content: %s)", err, truncate(resp.Content, 300))
	}

	meta := domain.SummaryMetadata{
		Emotions:   normalizeStringList(parsed.Emotions, 6),
		MoodScore:  parsed.MoodScore,
		MoodLabel:  strings.ToLower(strings.TrimSpace(parsed.MoodLabel)),
		Topics:     normalizeStringList(parsed.Topics, 5),
		EntryCount: len(rows),
	}
	if _, err := w.Summaries.Upsert(
		ctx, user.ID, string(domain.PeriodDay),
		period.Start, period.End,
		strings.TrimSpace(parsed.Body),
		meta,
		resp.Model,
		resp.PromptTokens, resp.CompletionTokens,
	); err != nil {
		return fmt.Errorf("upsert summary: %w", err)
	}
	return w.complete(ctx, job)
}

// ---------------- Weekly / Monthly / Yearly (text-only LLM, computed metadata) ----------------

func (w *SummaryWorker) runWeekly(
	ctx context.Context, job *domain.SummaryJob, user *domain.User, period timezone.Period,
) error {
	dailies, err := w.Summaries.ListDailyInRange(ctx, user.ID, period.Start, period.End)
	if err != nil {
		return fmt.Errorf("load daily summaries: %w", err)
	}
	if len(dailies) == 0 {
		return w.skip(ctx, job)
	}

	children := make([]weeklyDailyChild, 0, len(dailies))
	for _, d := range dailies {
		children = append(children, weeklyDailyChild{
			Date:      d.PeriodStart,
			Body:      d.Body,
			MoodLabel: emptyOr(d.Metadata.MoodLabel, "—"),
			MoodScore: formatMoodScore(d.Metadata.MoodScore),
			Topics:    strings.Join(d.Metadata.Topics, ", "),
		})
	}
	userPrompt, err := renderTemplate("weekly.tmpl", map[string]any{
		"PeriodStart":     period.Start.Format("2006-01-02"),
		"PeriodEnd":       period.End.Format("2006-01-02"),
		"DailySummaries":  children,
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
	meta := aggregateMetadata(dailies)
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

func (w *SummaryWorker) runMonthly(
	ctx context.Context, job *domain.SummaryJob, user *domain.User, period timezone.Period,
) error {
	weeklies, err := w.Summaries.ListInRange(ctx, user.ID, string(domain.PeriodWeek), period.Start, period.End)
	if err != nil {
		return fmt.Errorf("load weekly summaries: %w", err)
	}
	if len(weeklies) == 0 {
		return w.skip(ctx, job)
	}

	children := make([]monthlyWeeklyChild, 0, len(weeklies))
	for _, ws := range weeklies {
		children = append(children, monthlyWeeklyChild{
			PeriodStart: ws.PeriodStart,
			Body:        ws.Body,
		})
	}
	userPrompt, err := renderTemplate("monthly.tmpl", map[string]any{
		"MonthLabel":      period.Start.Format("January 2006"),
		"PeriodStart":     period.Start.Format("2006-01-02"),
		"PeriodEnd":       period.End.Format("2006-01-02"),
		"WeeklySummaries": children,
	})
	if err != nil {
		return err
	}
	resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
		System:    monthlySystemPrompt,
		User:      userPrompt,
		MaxTokens: monthlyMaxTokens,
	})
	if err != nil {
		return err
	}
	meta := aggregateMetadata(weeklies)
	if _, err := w.Summaries.Upsert(
		ctx, user.ID, string(domain.PeriodMonth),
		period.Start, period.End,
		strings.TrimSpace(resp.Content), meta,
		resp.Model, resp.PromptTokens, resp.CompletionTokens,
	); err != nil {
		return fmt.Errorf("upsert summary: %w", err)
	}
	return w.complete(ctx, job)
}

func (w *SummaryWorker) runYearly(
	ctx context.Context, job *domain.SummaryJob, user *domain.User, period timezone.Period,
) error {
	monthlies, err := w.Summaries.ListInRange(ctx, user.ID, string(domain.PeriodMonth), period.Start, period.End)
	if err != nil {
		return fmt.Errorf("load monthly summaries: %w", err)
	}
	if len(monthlies) == 0 {
		return w.skip(ctx, job)
	}

	children := make([]yearlyMonthlyChild, 0, len(monthlies))
	for _, m := range monthlies {
		mt, _ := time.Parse("2006-01-02", m.PeriodStart)
		children = append(children, yearlyMonthlyChild{
			MonthLabel: mt.Format("January"),
			Body:       m.Body,
		})
	}
	userPrompt, err := renderTemplate("yearly.tmpl", map[string]any{
		"Year":             period.Start.Year(),
		"MonthlySummaries": children,
	})
	if err != nil {
		return err
	}
	resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
		System:    yearlySystemPrompt,
		User:      userPrompt,
		MaxTokens: yearlyMaxTokens,
	})
	if err != nil {
		return err
	}
	meta := aggregateMetadata(monthlies)
	if _, err := w.Summaries.Upsert(
		ctx, user.ID, string(domain.PeriodYear),
		period.Start, period.End,
		strings.TrimSpace(resp.Content), meta,
		resp.Model, resp.PromptTokens, resp.CompletionTokens,
	); err != nil {
		return fmt.Errorf("upsert summary: %w", err)
	}
	return w.complete(ctx, job)
}

// ---------------- Lifecycle helpers ----------------

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

// ---------------- Aggregation ----------------

// aggregateMetadata rolls per-child metadata into the parent period's
// metadata. Mood is a weighted average by entry count; emotions/topics
// are top-N by frequency. We compute these in code rather than asking
// the LLM to do arithmetic over invisible numeric fields.
func aggregateMetadata(children []domain.Summary) domain.SummaryMetadata {
	emotionFreq := map[string]int{}
	topicFreq := map[string]int{}
	var totalScore, totalWeight float64
	totalEntries := 0
	posCount, negCount, neuCount := 0, 0, 0

	for _, c := range children {
		for _, e := range c.Metadata.Emotions {
			emotionFreq[strings.ToLower(strings.TrimSpace(e))]++
		}
		for _, t := range c.Metadata.Topics {
			topicFreq[strings.ToLower(strings.TrimSpace(t))]++
		}
		w := float64(c.Metadata.EntryCount)
		if w == 0 {
			w = 1
		}
		if c.Metadata.MoodScore != nil {
			totalScore += *c.Metadata.MoodScore * w
			totalWeight += w
		}
		switch c.Metadata.MoodLabel {
		case "positive":
			posCount++
		case "negative":
			negCount++
		case "neutral":
			neuCount++
		}
		totalEntries += c.Metadata.EntryCount
	}

	out := domain.SummaryMetadata{
		Emotions:   topNByFrequency(emotionFreq, 6),
		Topics:     topNByFrequency(topicFreq, 5),
		EntryCount: totalEntries,
	}
	if totalWeight > 0 {
		avg := totalScore / totalWeight
		out.MoodScore = &avg
	}
	out.MoodLabel = pickLabel(posCount, neuCount, negCount)
	return out
}

func topNByFrequency(freq map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(freq))
	for k, v := range freq {
		if k == "" {
			continue
		}
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p.k)
	}
	return out
}

func pickLabel(pos, neu, neg int) string {
	if pos == 0 && neu == 0 && neg == 0 {
		return ""
	}
	if pos >= neu && pos >= neg {
		return "positive"
	}
	if neg >= pos && neg >= neu {
		return "negative"
	}
	return "neutral"
}

// ---------------- JSON helpers ----------------

func parseDailyJSON(content string) (*dailyLLMResponse, error) {
	cleaned := stripFences(content)
	var out dailyLLMResponse
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, err
	}
	if out.Body == "" {
		return nil, errors.New("empty body field")
	}
	return &out, nil
}

// stripFences removes ```json ... ``` wrappers some models still emit
// even when asked for raw JSON. Idempotent on un-fenced input.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the first line ("```json" or "```") and trailing fence.
	if nl := strings.Index(s, "\n"); nl >= 0 {
		s = s[nl+1:]
	}
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func normalizeStringList(in []string, maxLen int) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
		if len(out) >= maxLen {
			break
		}
	}
	return out
}

func emptyOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func formatMoodScore(score *float64) string {
	if score == nil {
		return "—"
	}
	return fmt.Sprintf("%.0f", *score)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
