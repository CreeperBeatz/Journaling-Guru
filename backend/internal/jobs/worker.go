package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/riverqueue/river"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// Per-pass output token caps for the weekly synthesis pipeline.
// Conservative — we'd rather see a clipped paragraph than a hallucinated
// next-paragraph. Sized for the JSON envelope plus content. The original
// combined cap was 1200; splitting lets each pass have a tighter ceiling.
const (
	weeklyStructuredMaxTokens = 600  // per_day_tags + themes
	weeklyNarrativeMaxTokens  = 1500 // headline + 4 paragraphs + closing question
	monthlyNarrativeMaxTokens = 1500 // headline + 3 paragraphs + direction question
)

// SummaryWorker handles SummaryArgs jobs. Under the Energy Audit pivot
// the weekly period survives, plus the monthly synthesis (re-admitted by
// the monthly reflection loop — it composes hierarchically over weekly
// artifacts, never raw entries); the daily / yearly LLM paths stay
// retired (CHECK constraint on summary_jobs.period_type enforces this
// at the DB layer too).
type SummaryWorker struct {
	river.WorkerDefaults[SummaryArgs]

	Summaries          *store.SummaryStore
	Jobs               *store.SummaryJobStore
	Entries            *store.EntryStore
	DailyInputs        *store.DailyInputStore
	DailyEntryTags     *store.DailyEntryTagStore
	Tags               *store.TagStore
	Users              *store.UserStore
	WeeklyReflections  *store.WeeklyReflectionStore
	MonthlyReflections *store.MonthlyReflectionStore
	Goals              *store.GoalStore
	Scheduler          *Scheduler
	LLM                *llm.OpenRouter
	Logger             *slog.Logger

	// ShotCount is the number of parallel narrative shots in the
	// weekly synthesis ensemble. <=1 means single-shot (skip the
	// combiner); >=2 fans out N narrative calls and merges them via
	// the combiner. Populated from cfg.SummaryShotCount in main.go.
	ShotCount int
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
		periodStart, user.Timezone, user.DayStartMinutes, user.ReflectionWeekday,
		domain.SummaryPeriod(job.PeriodType),
	)
	if err != nil {
		return fmt.Errorf("recompute period: %w", err)
	}
	switch domain.SummaryPeriod(job.PeriodType) {
	case domain.PeriodWeek:
		return w.runWeekly(ctx, job, user, period)
	case domain.PeriodMonth:
		return w.runMonthly(ctx, job, user, period)
	default:
		// Defensive: the DB CHECK only allows 'week'/'month', but if a
		// stale row sneaks through (e.g. a paused job from before the
		// constraint tightened), skip it instead of failing.
		w.Logger.Info("skipping retired period type",
			"job_id", job.ID, "period", job.PeriodType)
		return w.skip(ctx, job)
	}
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
	// is the job's own period (period.Start..period.End inclusive) —
	// NOT "last N days from now": jobs can fire days late (retries,
	// worker downtime, carry-over) and a relative window would silently
	// aggregate the wrong week.
	var drainers, chargers []store.TagAggregate
	if w.DailyEntryTags != nil {
		drainers, _ = w.DailyEntryTags.TopByValenceInRange(ctx, user.ID, domain.TagRoleDrainer, period.Start, period.End, 5)
		chargers, _ = w.DailyEntryTags.TopByValenceInRange(ctx, user.ID, domain.TagRoleCharger, period.Start, period.End, 5)
	}

	moodSeries, _ := w.DailyInputs.MoodSeriesInRange(ctx, user.ID, period.Start, period.End)
	gratitudes, _ := w.DailyInputs.ListGratitudeInRange(ctx, user.ID, period.Start, period.End)

	// Per-day text snapshot. Days with content but no existing tags get
	// fed to the LLM for ad-hoc tag extraction inside the same call that
	// produces the synthesis. Manual-wins: days that already have tags
	// (from chat extraction or the manual TagPicker) are skipped here
	// and only contribute via the aggregate table above.
	needsExtraction := w.collectDaysNeedingExtraction(ctx, user.ID, period.Start, period.End)

	// Existing taxonomy passed in so the LLM prefers reusing canonical
	// labels rather than inventing near-duplicates ("morning walks" vs
	// "morning walk"). Cap the list — Gemma's prompt budget isn't huge.
	taxonomy := w.collectTagTaxonomy(ctx, user.ID, 40)

	// Additional free-text sources for the "insights" paragraph. Errors
	// here degrade the paragraph but never fail the whole synthesis —
	// the model is asked to emit "" when these are thin anyway.
	dailyNotes, _ := w.DailyInputs.ListNotesInRange(ctx, user.ID, period.Start, period.End)
	manualEntries, _ := w.Entries.ListInRangeWithPrompts(ctx, user.ID, period.Start, period.End)
	var priorReflection *domain.WeeklyReflection
	if w.WeeklyReflections != nil {
		priorReflection, _ = w.WeeklyReflections.LatestBeforeWeek(ctx, user.ID, period.Start)
	}

	userPrompt := buildWeeklySynthesisPrompt(period, agg, drainers, chargers, moodSeries, gratitudes, needsExtraction, taxonomy, dailyNotes, manualEntries, priorReflection)

	// --- Structured pass: 1 call. Extracts per_day_tags + themes.
	// Mechanical extraction — doesn't benefit from ensembling, so it
	// stays single-shot regardless of ShotCount.
	structuredResp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
		System:    weeklyStructuredSystemPrompt,
		User:      userPrompt,
		MaxTokens: weeklyStructuredMaxTokens,
		JSONMode:  true,
	})
	if err != nil {
		return fmt.Errorf("structured pass: %w", err)
	}
	structured := parseWeeklyStructured(structuredResp.Content, w.Logger, job.ID)

	// --- Narrative pass: N parallel shots + combiner. When ShotCount
	// <= 1, falls back to a single shot and skips the combiner — the
	// pipeline still benefits from the structured/narrative split, but
	// without the ensemble cost.
	shotCount := w.ShotCount
	if shotCount < 1 {
		shotCount = 1
	}
	var (
		narrative  parsedNarrative
		narrModel  string
		narrPrompt int
		narrCompl  int
	)
	if shotCount == 1 {
		resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
			System:    weeklyNarrativeSystemPrompt,
			User:      userPrompt,
			MaxTokens: weeklyNarrativeMaxTokens,
			JSONMode:  true,
		})
		if err != nil {
			return fmt.Errorf("narrative pass: %w", err)
		}
		narrative = parseWeeklyNarrative(resp.Content, w.Logger, job.ID)
		narrModel = resp.Model
		narrPrompt = resp.PromptTokens
		narrCompl = resp.CompletionTokens
	} else {
		candidates, shotsUsage, err := w.runNarrativeShots(ctx, userPrompt, shotCount, job.ID)
		if err != nil {
			return fmt.Errorf("narrative shots: %w", err)
		}
		w.Logger.Info("narrative shots complete",
			"job_id", job.ID,
			"shot_count", shotCount,
			"succeeded", len(candidates))

		combinerResp, combineErr := w.LLM.Complete(ctx, llm.CompletionRequest{
			System:    weeklyCombinerSystemPrompt,
			User:      buildCombinerPrompt(candidates),
			MaxTokens: weeklyNarrativeMaxTokens,
			JSONMode:  true,
		})
		if combineErr != nil {
			w.Logger.Warn("combiner failed; falling back to best candidate",
				"err", combineErr, "job_id", job.ID)
			narrative = pickBestCandidate(candidates)
			narrModel = shotsUsage.model
			narrPrompt = shotsUsage.prompt
			narrCompl = shotsUsage.completion
		} else {
			parsedCombined := parseWeeklyNarrative(combinerResp.Content, w.Logger, job.ID)
			if !parsedCombined.hasContent() {
				w.Logger.Warn("combiner returned empty content; falling back to best candidate",
					"job_id", job.ID)
				narrative = pickBestCandidate(candidates)
				narrModel = shotsUsage.model
				narrPrompt = shotsUsage.prompt
				narrCompl = shotsUsage.completion
			} else {
				narrative = parsedCombined
				narrModel = combinerResp.Model
				narrPrompt = shotsUsage.prompt + combinerResp.PromptTokens
				narrCompl = shotsUsage.completion + combinerResp.CompletionTokens
			}
		}
	}

	// Persist per-day tag extractions. Manual-wins is enforced by only
	// asking the LLM about days that lacked tags AND by guarding the
	// write side here too — if a day acquired tags between the read
	// above and now (cross-tab race), we don't clobber.
	w.persistPerDayTags(ctx, user.ID, structured.PerDayTags, needsExtraction)

	meta := domain.SummaryMetadata{
		EntryCount:      agg.EntryCount,
		MoodScore:       agg.MoodScore,
		Letter:          narrative.Letter, // legacy fallback; empty when structured paragraphs exist
		Charged:         narrative.Charged,
		Drained:         narrative.Drained,
		Grateful:        narrative.Grateful,
		Insights:        narrative.Insights,
		Themes:          structured.Themes,
		ClosingQuestion: narrative.ClosingQuestion,
	}
	if agg.MoodScore != nil {
		// math.Round, not int(x+0.5): the latter truncates toward zero
		// and mis-rounds negative averages on the signed -2..+2 scale.
		rounded := int(math.Round(*agg.MoodScore))
		meta.MoodLabel = domain.MoodLabel(&rounded)
	}

	// Token accounting: sum all calls (structured + narrative shots +
	// combiner) into the single prompt_tokens / completion_tokens
	// columns on summaries. The schema has one slot per row; operators
	// care about total cost.
	totalPromptTokens := structuredResp.PromptTokens + narrPrompt
	totalCompletionTokens := structuredResp.CompletionTokens + narrCompl

	if _, err := w.Summaries.Upsert(
		ctx, user.ID, string(domain.PeriodWeek),
		period.Start, period.End,
		narrative.Headline, meta,
		narrModel, totalPromptTokens, totalCompletionTokens,
	); err != nil {
		return fmt.Errorf("upsert summary: %w", err)
	}
	return w.complete(ctx, job)
}

// runMonthly synthesizes the monthly letter — hierarchically, from the
// month's weekly letters + reflections + goal ledger + mood/ratings
// trends, NEVER raw daily entries. One structured JSON call, no
// ensemble: the inputs are already distilled, so N-shot diversity buys
// little here.
//
// Fire ordering with the final weekly job (same morning, +15min) is
// best-effort: a missing final weekly letter degrades the input set, it
// never blocks. Don't add a wait-for-weekly loop.
func (w *SummaryWorker) runMonthly(
	ctx context.Context, job *domain.SummaryJob, user *domain.User, period timezone.Period,
) error {
	// The month's weekly letters: overlap (not period_start containment)
	// because the final week of June can end on July 5.
	allWeeklies, err := w.Summaries.ListOverlappingRange(
		ctx, user.ID, string(domain.PeriodWeek), period.Start, period.End)
	if err != nil {
		return fmt.Errorf("list weeklies: %w", err)
	}
	weeklies := make([]domain.Summary, 0, len(allWeeklies))
	for _, s := range allWeeklies {
		if s.Metadata.HasLetterSynthesis() {
			weeklies = append(weeklies, s)
		}
	}
	// No weekly letters means nothing to compose at this altitude —
	// weekly summaries auto-generate for any journaled week, so an empty
	// set means an empty (or fully-skipped) month.
	if len(weeklies) == 0 {
		return w.skip(ctx, job)
	}

	// Weekly reflections in the month (their week_start can begin up to
	// 6 days before month start) — surprise_texts are continuity notes.
	var reflections []domain.WeeklyReflection
	if w.WeeklyReflections != nil {
		reflections, _ = w.WeeklyReflections.ListInRange(
			ctx, user.ID, period.Start.AddDate(0, 0, -6), period.End)
	}

	// Goal ledger: everything alive or resolved during the month.
	var ledger []domain.Goal
	if w.Goals != nil {
		ledger, _ = w.Goals.ListTouchedInRange(ctx, user.ID, period.Start, period.End)
	}

	// Mood trend: this month vs the prior month.
	agg, err := w.DailyInputs.AggregateForRange(ctx, user.ID, period.Start, period.End)
	if err != nil {
		return fmt.Errorf("aggregate: %w", err)
	}
	prevAgg, _ := w.DailyInputs.AggregateForRange(
		ctx, user.ID, period.Start.AddDate(0, -1, 0), period.Start.AddDate(0, 0, -1))

	// Last month's intention + direction (continuity: the letter must
	// reckon with the intention), and the prior ratings trend. The
	// CURRENT month's ratings are never an input — the user rates after
	// reading this letter.
	var prior *domain.MonthlyReflection
	var ratingsTrend []domain.MonthlyReflection
	if w.MonthlyReflections != nil {
		prior, _ = w.MonthlyReflections.LatestBefore(ctx, user.ID, period.Start)
		ratingsTrend, _ = w.MonthlyReflections.ListRatingsInRange(
			ctx, user.ID, period.Start.AddDate(-1, 0, 0), period.Start.AddDate(0, 0, -1))
	}

	userPrompt := buildMonthlySynthesisPrompt(
		period, weeklies, reflections, ledger, agg, prevAgg, prior, ratingsTrend)

	resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
		System:    monthlyNarrativeSystemPrompt,
		User:      userPrompt,
		MaxTokens: monthlyNarrativeMaxTokens,
		JSONMode:  true,
	})
	if err != nil {
		return fmt.Errorf("monthly narrative: %w", err)
	}
	narrative := parseMonthlyNarrative(resp.Content, w.Logger, job.ID)

	meta := domain.SummaryMetadata{
		EntryCount:      agg.EntryCount,
		MoodScore:       agg.MoodScore,
		Arc:             narrative.Arc,
		Recurring:       narrative.Recurring,
		GoalsRetro:      narrative.GoalsRetro,
		ClosingQuestion: narrative.ClosingQuestion,
	}
	if agg.MoodScore != nil {
		rounded := int(math.Round(*agg.MoodScore))
		meta.MoodLabel = domain.MoodLabel(&rounded)
	}

	if _, err := w.Summaries.Upsert(
		ctx, user.ID, string(domain.PeriodMonth),
		period.Start, period.End,
		narrative.Headline, meta,
		resp.Model, resp.PromptTokens, resp.CompletionTokens,
	); err != nil {
		return fmt.Errorf("upsert summary: %w", err)
	}
	return w.complete(ctx, job)
}

// monthlyNarrativeRaw is the JSON envelope the monthly synthesis emits.
type monthlyNarrativeRaw struct {
	Headline        string `json:"headline"`
	Arc             string `json:"arc"`
	Recurring       string `json:"recurring"`
	GoalsRetro      string `json:"goals_retro"`
	ClosingQuestion string `json:"closing_question"`
}

// parsedMonthlyNarrative is the worker-side normalized view. Paragraphs
// may be empty — the prompt explicitly allows "" over padding.
type parsedMonthlyNarrative struct {
	Headline        string
	Arc             string
	Recurring       string
	GoalsRetro      string
	ClosingQuestion string
}

// parseMonthlyNarrative mirrors parseWeeklyNarrative's degradation: on
// malformed JSON the raw content becomes the Arc paragraph with a
// first-sentence headline, so a misbehaving call still renders.
func parseMonthlyNarrative(raw string, logger *slog.Logger, jobID string) parsedMonthlyNarrative {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return parsedMonthlyNarrative{}
	}
	var rawParsed monthlyNarrativeRaw
	if err := json.Unmarshal([]byte(trimmed), &rawParsed); err != nil {
		logger.Warn("monthly narrative JSON parse failed; degrading",
			"job_id", jobID, "err", err)
		return parsedMonthlyNarrative{
			Headline: firstSentence(trimmed),
			Arc:      trimmed,
		}
	}
	out := parsedMonthlyNarrative{
		Headline:        strings.TrimSpace(rawParsed.Headline),
		Arc:             strings.TrimSpace(rawParsed.Arc),
		Recurring:       strings.TrimSpace(rawParsed.Recurring),
		GoalsRetro:      strings.TrimSpace(rawParsed.GoalsRetro),
		ClosingQuestion: strings.TrimSpace(rawParsed.ClosingQuestion),
	}
	if out.Headline == "" {
		switch {
		case out.Arc != "":
			out.Headline = firstSentence(out.Arc)
		case out.Recurring != "":
			out.Headline = firstSentence(out.Recurring)
		case out.GoalsRetro != "":
			out.Headline = firstSentence(out.GoalsRetro)
		}
	}
	return out
}

// buildMonthlySynthesisPrompt assembles the user-message body for the
// monthly synthesis call. Everything here is week-altitude or above —
// the system prompt forbids inventing day-level specifics, so none are
// provided.
func buildMonthlySynthesisPrompt(
	period timezone.Period,
	weeklies []domain.Summary,
	reflections []domain.WeeklyReflection,
	ledger []domain.Goal,
	agg, prevAgg *store.AggregatedMetadata,
	prior *domain.MonthlyReflection,
	ratingsTrend []domain.MonthlyReflection,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Month: %s (%s to %s).\n\n",
		period.Start.Format("January 2006"),
		period.Start.Format("2006-01-02"), period.End.Format("2006-01-02"))

	if agg != nil && agg.MoodScore != nil {
		fmt.Fprintf(&b, "Average mood this month (-2..+2 scale): %+.2f over %d logged day(s).\n",
			*agg.MoodScore, agg.EntryCount)
	} else {
		b.WriteString("Average mood this month: not enough data.\n")
	}
	if prevAgg != nil && prevAgg.MoodScore != nil {
		fmt.Fprintf(&b, "Average mood the month before: %+.2f over %d logged day(s).\n",
			*prevAgg.MoodScore, prevAgg.EntryCount)
	}
	b.WriteString("\n")

	// Surprise texts keyed by week_start so each week block can carry its
	// own reflection note.
	surpriseByWeek := make(map[string]string, len(reflections))
	for _, r := range reflections {
		if s := strings.TrimSpace(r.SurpriseText); s != "" {
			surpriseByWeek[r.WeekStart] = s
		}
	}

	b.WriteString("## The month's weekly letters (oldest first)\n")
	b.WriteString("Each block is one week's synthesis, written at the time.\n\n")
	for i, s := range weeklies {
		fmt.Fprintf(&b, "### Week %d: %s to %s\n", i+1, s.PeriodStart, s.PeriodEnd)
		if s.Metadata.MoodScore != nil {
			fmt.Fprintf(&b, "  mood: %+.2f\n", *s.Metadata.MoodScore)
		}
		if s.Body != "" {
			fmt.Fprintf(&b, "  headline: %s\n", truncateForPrompt(s.Body, 300))
		}
		for _, p := range []struct{ label, text string }{
			{"charged", s.Metadata.Charged},
			{"drained", s.Metadata.Drained},
			{"grateful", s.Metadata.Grateful},
			{"insights", s.Metadata.Insights},
			{"letter", s.Metadata.Letter}, // legacy rows only
		} {
			if p.text != "" {
				fmt.Fprintf(&b, "  %s: %s\n", p.label, truncateForPrompt(p.text, 900))
			}
		}
		if len(s.Metadata.Themes) > 0 {
			names := make([]string, 0, len(s.Metadata.Themes))
			for _, t := range s.Metadata.Themes {
				names = append(names, t.Name)
			}
			fmt.Fprintf(&b, "  themes: %s\n", strings.Join(names, ", "))
		}
		if surprise, ok := surpriseByWeek[s.PeriodStart]; ok {
			fmt.Fprintf(&b, "  the user's own reflection note that week: %s\n",
				truncateForPrompt(surprise, 600))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Goal ledger this month\n")
	if len(ledger) == 0 {
		b.WriteString("  (no goals were alive or resolved this month)\n")
	} else {
		for _, g := range ledger {
			line := fmt.Sprintf("  - %q (%s to %s, status: %s", g.Title, g.StartDate, g.EndDate, g.Status)
			if g.Outcome != nil {
				line += ", outcome: " + *g.Outcome
			}
			line += ")"
			b.WriteString(line + "\n")
			if why := strings.TrimSpace(g.WhyMatters); why != "" {
				fmt.Fprintf(&b, "      why it mattered to them: %s\n", truncateForPrompt(why, 300))
			}
			if concl := strings.TrimSpace(g.ConclusionText); concl != "" {
				fmt.Fprintf(&b, "      their wrap-up note: %s\n", truncateForPrompt(concl, 300))
			}
		}
	}
	b.WriteString("\n")

	if prior != nil {
		fmt.Fprintf(&b, "## Last month's reflection (month starting %s)\n", prior.MonthStart)
		if intention := strings.TrimSpace(prior.IntentionText); intention != "" {
			fmt.Fprintf(&b, "  Their intention for THIS month, set back then: %s\n",
				truncateForPrompt(intention, 300))
			b.WriteString("  The arc paragraph must reckon with this intention honestly.\n")
		}
		if direction := strings.TrimSpace(prior.DirectionText); direction != "" {
			fmt.Fprintf(&b, "  Their direction note from back then: %s\n",
				truncateForPrompt(direction, 600))
		}
		b.WriteString("\n")
	}

	if len(ratingsTrend) > 0 {
		b.WriteString("## Life check-in ratings from PRIOR months (0-10 per domain)\n")
		b.WriteString("These are satisfaction sliders from past monthly reflections. Only\n")
		b.WriteString("treat a move of 2+ points as movement; ±1 is noise.\n\n")
		for _, mr := range ratingsTrend {
			parts := make([]string, 0, len(mr.Ratings))
			for _, d := range domain.LifeDomains {
				if score, ok := mr.Ratings[d.Key]; ok {
					parts = append(parts, fmt.Sprintf("%s %d", d.Label, score))
				}
			}
			fmt.Fprintf(&b, "  - %s: %s\n", mr.MonthStart, strings.Join(parts, ", "))
		}
		b.WriteString("\n")
	}

	b.WriteString(
		"Now emit the JSON object exactly per the schema in the system " +
			"prompt. Do not include any prose outside the JSON object.")
	return b.String()
}

// dayNeedingExtraction is one day in the week that has user-written
// drained/charged text but no daily_entry_tags rows yet. The weekly
// synthesis LLM is asked to mint canonical tags for these days inline
// with the rest of its output.
type dayNeedingExtraction struct {
	LocalDate string // YYYY-MM-DD
	Drained   string
	Charged   string
}

// collectDaysNeedingExtraction iterates the week day-by-day. A day is
// included when it has drained_text or charged_text on file AND no
// daily_entry_tags row of any role. Errors are logged and the day is
// skipped — better to ship a partial extraction list than to fail the
// whole synthesis job.
func (w *SummaryWorker) collectDaysNeedingExtraction(
	ctx context.Context, userID string, start, end time.Time,
) []dayNeedingExtraction {
	out := make([]dayNeedingExtraction, 0)
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		di, err := w.DailyInputs.GetByDate(ctx, userID, d)
		if err != nil || di == nil {
			continue
		}
		drained := strings.TrimSpace(di.DrainedText)
		charged := strings.TrimSpace(di.ChargedText)
		if drained == "" && charged == "" {
			continue
		}
		links, err := w.DailyEntryTags.ListByDate(ctx, userID, d)
		if err != nil {
			w.Logger.Warn("list day tags failed during extraction scan",
				"err", err, "user_id", userID, "date", di.LocalDate)
			continue
		}
		if len(links) > 0 {
			continue
		}
		out = append(out, dayNeedingExtraction{
			LocalDate: di.LocalDate,
			Drained:   drained,
			Charged:   charged,
		})
	}
	return out
}

// collectTagTaxonomy returns up to `limit` active tags across both
// valences for prompt context. Empty list when the user has nothing
// on file yet.
func (w *SummaryWorker) collectTagTaxonomy(ctx context.Context, userID string, limit int) []domain.Tag {
	if w.Tags == nil {
		return nil
	}
	out := make([]domain.Tag, 0, limit)
	for _, v := range []string{domain.TagValenceNegative, domain.TagValencePositive} {
		got, err := w.Tags.ListActiveByValence(ctx, userID, v)
		if err != nil {
			continue
		}
		for _, t := range got {
			if len(out) >= limit {
				break
			}
			out = append(out, t)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

// persistPerDayTags upserts each label and replaces the day's tag
// links for the named role. Only acts on dates that were in the
// "needs extraction" list — a defensive double-check against the LLM
// hallucinating tags for already-tagged days.
func (w *SummaryWorker) persistPerDayTags(
	ctx context.Context,
	userID string,
	perDay map[string]extractedDayTags,
	requested []dayNeedingExtraction,
) {
	if len(perDay) == 0 || len(requested) == 0 {
		return
	}
	allowed := make(map[string]struct{}, len(requested))
	for _, d := range requested {
		allowed[d.LocalDate] = struct{}{}
	}
	for dateStr, tags := range perDay {
		if _, ok := allowed[dateStr]; !ok {
			continue
		}
		localDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			w.Logger.Warn("invalid per_day_tags date; skipping",
				"err", err, "date", dateStr)
			continue
		}
		// Race guard: re-check that the day is still untagged before
		// writing. A chat extraction completing between the prompt
		// build and now should keep its result.
		if links, _ := w.DailyEntryTags.ListByDate(ctx, userID, localDate); len(links) > 0 {
			continue
		}
		drainerIDs := w.upsertTagIDs(ctx, userID, tags.Drainers, domain.TagValenceNegative)
		chargerIDs := w.upsertTagIDs(ctx, userID, tags.Chargers, domain.TagValencePositive)
		if err := w.DailyEntryTags.ReplaceForDay(ctx, userID, localDate, domain.TagRoleDrainer, drainerIDs); err != nil {
			w.Logger.Warn("replace drainers failed",
				"err", err, "user_id", userID, "date", dateStr)
		}
		if err := w.DailyEntryTags.ReplaceForDay(ctx, userID, localDate, domain.TagRoleCharger, chargerIDs); err != nil {
			w.Logger.Warn("replace chargers failed",
				"err", err, "user_id", userID, "date", dateStr)
		}
	}
}

func (w *SummaryWorker) upsertTagIDs(ctx context.Context, userID string, labels []string, valence string) []string {
	if w.Tags == nil || len(labels) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(labels))
	out := make([]string, 0, len(labels))
	for _, raw := range labels {
		label := strings.TrimSpace(raw)
		if label == "" {
			continue
		}
		norm := store.NormalizeTagLabel(label)
		if _, ok := seen[norm]; ok {
			continue
		}
		seen[norm] = struct{}{}
		tag, err := w.Tags.UpsertByLabel(ctx, userID, label, valence)
		if err != nil || tag == nil {
			w.Logger.Warn("upsert tag failed",
				"err", err, "user_id", userID, "label", label)
			continue
		}
		out = append(out, tag.ID)
	}
	return out
}

// extractedDayTags is one day's slot in the per_day_tags map the LLM
// returns alongside themes/letter.
type extractedDayTags struct {
	Drainers []string `json:"drainers"`
	Chargers []string `json:"chargers"`
}

// weeklyStructuredRaw is the JSON envelope the structured pass emits —
// only per_day_tags + themes. Validated/normalized in
// parseWeeklyStructured.
type weeklyStructuredRaw struct {
	PerDayTags map[string]extractedDayTags `json:"per_day_tags"`
	Themes     []domain.SummaryTheme       `json:"themes"`
}

// weeklyNarrativeRaw is the JSON envelope each narrative shot (and the
// combiner) emits — headline + four paragraphs + closing question. The
// `Letter` field is the legacy single-blob shape kept as a fallback for
// models that ignore the schema and return one prose blob.
type weeklyNarrativeRaw struct {
	Headline        string `json:"headline"`
	Letter          string `json:"letter"`
	Charged         string `json:"charged"`
	Drained         string `json:"drained"`
	Grateful        string `json:"grateful"`
	Insights        string `json:"insights"`
	ClosingQuestion string `json:"closing_question"`
}

// parsedStructured is the worker-side normalized view of the
// structured pass's output. Themes is always non-nil so callers can
// range over it without a nil check.
type parsedStructured struct {
	PerDayTags map[string]extractedDayTags
	Themes     []domain.SummaryTheme
}

// parsedNarrative is the worker-side normalized view of one narrative
// shot (or the combiner's merged output). All four paragraphs may be
// empty strings — the prompt explicitly allows that when content is
// thin. Letter is the legacy fallback for models that ignored the
// schema; non-empty only when none of the structured paragraphs are.
type parsedNarrative struct {
	Headline        string
	Letter          string
	Charged         string
	Drained         string
	Grateful        string
	Insights        string
	ClosingQuestion string
}

// hasContent reports whether n has anything renderable. Used to drop
// empty narrative shots from the candidate pool before they reach the
// combiner.
func (n parsedNarrative) hasContent() bool {
	return n.Headline != "" || n.Letter != "" ||
		n.Charged != "" || n.Drained != "" ||
		n.Grateful != "" || n.Insights != ""
}

// parseWeeklyStructured extracts per_day_tags + themes from the
// structured pass's JSON. On malformed JSON it returns empty values
// (themes always non-nil) and logs at Warn — the worker keeps going
// without theme/per_day_tag persistence rather than failing the whole
// job.
func parseWeeklyStructured(raw string, logger *slog.Logger, jobID string) parsedStructured {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return parsedStructured{Themes: []domain.SummaryTheme{}}
	}
	var rawParsed weeklyStructuredRaw
	if err := json.Unmarshal([]byte(trimmed), &rawParsed); err != nil {
		logger.Warn("weekly structured JSON parse failed; degrading",
			"job_id", jobID, "err", err)
		return parsedStructured{Themes: []domain.SummaryTheme{}}
	}
	return parsedStructured{
		PerDayTags: rawParsed.PerDayTags,
		Themes:     normalizeThemes(rawParsed.Themes),
	}
}

// parseWeeklyNarrative extracts the headline + four paragraphs +
// closing question from one narrative shot's JSON. On malformed JSON
// it degrades the raw content into the legacy Letter blob with a
// first-sentence headline, so a misbehaving shot still produces
// something renderable downstream.
func parseWeeklyNarrative(raw string, logger *slog.Logger, jobID string) parsedNarrative {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return parsedNarrative{}
	}
	var rawParsed weeklyNarrativeRaw
	if err := json.Unmarshal([]byte(trimmed), &rawParsed); err != nil {
		logger.Warn("weekly narrative JSON parse failed; degrading",
			"job_id", jobID, "err", err)
		return parsedNarrative{
			Headline: firstSentence(trimmed),
			Letter:   trimmed,
		}
	}
	out := parsedNarrative{
		Headline:        strings.TrimSpace(rawParsed.Headline),
		Charged:         strings.TrimSpace(rawParsed.Charged),
		Drained:         strings.TrimSpace(rawParsed.Drained),
		Grateful:        strings.TrimSpace(rawParsed.Grateful),
		Insights:        strings.TrimSpace(rawParsed.Insights),
		ClosingQuestion: strings.TrimSpace(rawParsed.ClosingQuestion),
	}
	// Legacy fallback: if the model emitted one prose blob in `letter`
	// instead of the structured paragraphs, preserve it so the FE can
	// still render something. Drop it the moment any structured
	// paragraph exists — rendering both would duplicate content.
	rawLetter := strings.TrimSpace(rawParsed.Letter)
	if rawLetter != "" && out.Charged == "" && out.Drained == "" &&
		out.Grateful == "" && out.Insights == "" {
		out.Letter = rawLetter
	}
	if out.Headline == "" {
		// Some models skip the headline when the rule feels redundant.
		// Derive one from whichever body content exists.
		switch {
		case out.Charged != "":
			out.Headline = firstSentence(out.Charged)
		case out.Insights != "":
			out.Headline = firstSentence(out.Insights)
		case out.Drained != "":
			out.Headline = firstSentence(out.Drained)
		case out.Letter != "":
			out.Headline = firstSentence(out.Letter)
		}
	}
	return out
}

func normalizeThemes(in []domain.SummaryTheme) []domain.SummaryTheme {
	if len(in) == 0 {
		return []domain.SummaryTheme{}
	}
	out := make([]domain.SummaryTheme, 0, len(in))
	for _, t := range in {
		name := strings.TrimSpace(t.Name)
		if name == "" || len(t.Tags) == 0 {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(t.Role))
		switch role {
		case "drainer", "charger", "mixed":
		default:
			role = "mixed"
		}
		tags := make([]string, 0, len(t.Tags))
		for _, tag := range t.Tags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
		}
		if len(tags) == 0 {
			continue
		}
		out = append(out, domain.SummaryTheme{
			Name:         name,
			Tags:         tags,
			Role:         role,
			DaysAppeared: t.DaysAppeared,
			Note:         strings.TrimSpace(t.Note),
		})
	}
	return out
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Find the first '.', '!', or '?' that closes a sentence.
	for i, r := range s {
		switch r {
		case '.', '!', '?':
			return strings.TrimSpace(s[:i+1])
		case '\n':
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// narrativeUsage accumulates token counts and the response model name
// across the N parallel narrative shots. The combiner's usage is added
// separately at the call site; only the model name from one of the
// shots is kept (they all share the same configured model).
type narrativeUsage struct {
	prompt     int
	completion int
	model      string
}

// runNarrativeShots fires N parallel narrative LLM calls with identical
// system + user prompts, relying on the model's sampling stochasticity
// for diversity. Returns the successful candidates (failed shots are
// dropped and logged at Warn) and aggregated token usage. Returns an
// error only when ALL N shots fail — the caller propagates that to
// River as a job-level failure so the whole synthesis retries.
func (w *SummaryWorker) runNarrativeShots(
	ctx context.Context, userPrompt string, n int, jobID string,
) ([]parsedNarrative, narrativeUsage, error) {
	type result struct {
		parsed parsedNarrative
		resp   *llm.CompletionResponse
		err    error
	}
	results := make([]result, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
				System:    weeklyNarrativeSystemPrompt,
				User:      userPrompt,
				MaxTokens: weeklyNarrativeMaxTokens,
				JSONMode:  true,
			})
			if err != nil {
				results[i] = result{err: err}
				return
			}
			results[i] = result{
				parsed: parseWeeklyNarrative(resp.Content, w.Logger, jobID),
				resp:   resp,
			}
		}(i)
	}
	wg.Wait()

	candidates := make([]parsedNarrative, 0, n)
	var usage narrativeUsage
	for i, r := range results {
		if r.err != nil {
			w.Logger.Warn("narrative shot failed; skipping",
				"shot", i, "err", r.err, "job_id", jobID)
			continue
		}
		if !r.parsed.hasContent() {
			w.Logger.Warn("narrative shot returned empty content; skipping",
				"shot", i, "job_id", jobID)
			continue
		}
		candidates = append(candidates, r.parsed)
		usage.prompt += r.resp.PromptTokens
		usage.completion += r.resp.CompletionTokens
		usage.model = r.resp.Model
	}
	if len(candidates) == 0 {
		return nil, usage, fmt.Errorf("all %d narrative shots failed", n)
	}
	return candidates, usage, nil
}

// buildCombinerPrompt assembles the user-message body for the combiner
// LLM call. It receives the candidate narratives as a numbered JSON
// array; the system prompt's rules describe how to merge them. The
// combiner does NOT see the original aggregates — only the candidates
// — which keeps its prompt bounded and focuses it on synthesis rather
// than re-generation.
//
// Each candidate's paragraphs are truncated to 600 chars as a defensive
// cap. In-spec outputs are well under that; truncation only kicks in
// when a shot wildly ignored the ≤80-word rule.
func buildCombinerPrompt(candidates []parsedNarrative) string {
	type candidateOut struct {
		Headline        string `json:"headline,omitempty"`
		Charged         string `json:"charged,omitempty"`
		Drained         string `json:"drained,omitempty"`
		Grateful        string `json:"grateful,omitempty"`
		Insights        string `json:"insights,omitempty"`
		ClosingQuestion string `json:"closing_question,omitempty"`
	}
	const cap = 600
	payload := make([]candidateOut, 0, len(candidates))
	for _, c := range candidates {
		payload = append(payload, candidateOut{
			Headline:        truncateForPrompt(c.Headline, cap),
			Charged:         truncateForPrompt(c.Charged, cap),
			Drained:         truncateForPrompt(c.Drained, cap),
			Grateful:        truncateForPrompt(c.Grateful, cap),
			Insights:        truncateForPrompt(c.Insights, cap),
			ClosingQuestion: truncateForPrompt(c.ClosingQuestion, cap),
		})
	}
	enc, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		// json.MarshalIndent on string-only structs cannot fail in
		// practice; the fallback is defensive belt-and-braces.
		var b strings.Builder
		for i, c := range payload {
			fmt.Fprintf(&b, "## Candidate %d\n", i+1)
			fmt.Fprintf(&b, "headline: %s\n", c.Headline)
			fmt.Fprintf(&b, "charged: %s\n", c.Charged)
			fmt.Fprintf(&b, "drained: %s\n", c.Drained)
			fmt.Fprintf(&b, "grateful: %s\n", c.Grateful)
			fmt.Fprintf(&b, "insights: %s\n", c.Insights)
			fmt.Fprintf(&b, "closing_question: %s\n\n", c.ClosingQuestion)
		}
		return b.String()
	}
	return fmt.Sprintf("Here are %d candidate weekly reflections, each emitted by a different "+
		"therapist responding to the same week. Merge them into one definitive "+
		"reflection per the rules in the system prompt.\n\n%s\n\nNow emit the merged "+
		"JSON object. Do not include any prose outside the JSON.",
		len(candidates), string(enc))
}

// pickBestCandidate returns the candidate with the most non-empty
// paragraphs (tiebreak: longest total prose). Used as a fallback when
// the combiner fails or returns empty content — we'd rather ship one
// of the parallel shots verbatim than fail the whole synthesis.
func pickBestCandidate(candidates []parsedNarrative) parsedNarrative {
	if len(candidates) == 0 {
		return parsedNarrative{}
	}
	bestIdx := 0
	bestScore, bestLen := -1, -1
	for i, c := range candidates {
		score := 0
		for _, p := range []string{c.Charged, c.Drained, c.Grateful, c.Insights} {
			if p != "" {
				score++
			}
		}
		length := len(c.Charged) + len(c.Drained) + len(c.Grateful) + len(c.Insights) + len(c.Letter)
		if score > bestScore || (score == bestScore && length > bestLen) {
			bestIdx, bestScore, bestLen = i, score, length
		}
	}
	return candidates[bestIdx]
}

// buildWeeklySynthesisPrompt assembles the user-message body for the
// weekly synthesis LLM calls. Both the structured pass and each
// narrative shot receive this same payload — the system prompt for each
// role tells the model which fields to emit. Field naming mirrors the
// schemas documented in weeklyStructuredSystemPrompt and
// weeklyNarrativeSystemPrompt.
//
// The dailyNotes / manualEntries / priorReflection sources feed the
// "insights" paragraph specifically. They are kept in their own labelled
// sections so the model can cite the source briefly ("from your Tuesday
// note…").
func buildWeeklySynthesisPrompt(
	period timezone.Period,
	agg *store.AggregatedMetadata,
	drainers, chargers []store.TagAggregate,
	moodSeries []store.DailyMoodPoint,
	gratitudes []store.GratitudeRow,
	needsExtraction []dayNeedingExtraction,
	taxonomy []domain.Tag,
	dailyNotes []store.DailyNoteRow,
	manualEntries []store.EntryWithPromptAndDate,
	priorReflection *domain.WeeklyReflection,
) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Week of %s to %s.\n\n",
		period.Start.Format("2006-01-02"), period.End.Format("2006-01-02"))

	if agg != nil && agg.MoodScore != nil {
		fmt.Fprintf(&b, "Average mood (-2..+2 scale: -2=very bad, 0=neutral, +2=very good): %+.2f over %d logged day(s).\n\n",
			*agg.MoodScore, agg.EntryCount)
	} else {
		b.WriteString("Average mood: not enough data this week.\n\n")
	}

	if len(moodSeries) > 0 {
		b.WriteString("Mood by day:\n")
		for _, p := range moodSeries {
			fmt.Fprintf(&b, "  - %s: %+.0f\n", p.LocalDate, p.Score)
		}
		b.WriteString("\n")
	}

	b.WriteString("Drainer tags (label, days appeared, avg mood on those days):\n")
	if len(drainers) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, d := range drainers {
			moodStr := "—"
			if d.AvgMood != nil {
				moodStr = strings.TrimRight(strings.TrimRight(strconvFloat(*d.AvgMood, 1), "0"), ".")
			}
			fmt.Fprintf(&b, "  - %q (%d day%s, mood %s)\n", d.Label, d.Appearances, plural3(d.Appearances), moodStr)
		}
	}
	b.WriteString("\nCharger tags (same shape):\n")
	if len(chargers) == 0 {
		b.WriteString("  (none)\n")
	} else {
		for _, c := range chargers {
			moodStr := "—"
			if c.AvgMood != nil {
				moodStr = strings.TrimRight(strings.TrimRight(strconvFloat(*c.AvgMood, 1), "0"), ".")
			}
			fmt.Fprintf(&b, "  - %q (%d day%s, mood %s)\n", c.Label, c.Appearances, plural3(c.Appearances), moodStr)
		}
	}

	if len(gratitudes) > 0 {
		b.WriteString("\nGratitude items (verbatim, day-tagged):\n")
		for _, g := range gratitudes {
			snippet := strings.TrimSpace(g.Text)
			if len(snippet) > 200 {
				snippet = snippet[:200] + "…"
			}
			fmt.Fprintf(&b, "  - %s: %s\n", g.LocalDate, snippet)
		}
	}

	if len(taxonomy) > 0 {
		b.WriteString("\n## Existing tag taxonomy (prefer reusing these labels verbatim)\n")
		for _, t := range taxonomy {
			role := "drainer"
			if t.Valence == domain.TagValencePositive {
				role = "charger"
			}
			fmt.Fprintf(&b, "  - %q (%s)\n", t.Label, role)
		}
	}

	if len(needsExtraction) > 0 {
		b.WriteString("\n## Days needing tag extraction\n")
		b.WriteString("Each entry below is a day with the user's verbatim drained/charged text\n")
		b.WriteString("but no tags on file. Emit a per_day_tags entry for each of these dates.\n\n")
		for _, d := range needsExtraction {
			fmt.Fprintf(&b, "### %s\n", d.LocalDate)
			if d.Drained != "" {
				fmt.Fprintf(&b, "  drained: %s\n", truncateForPrompt(d.Drained, 800))
			}
			if d.Charged != "" {
				fmt.Fprintf(&b, "  charged: %s\n", truncateForPrompt(d.Charged, 800))
			}
			b.WriteString("\n")
		}
	}

	// Free-text sources for the `insights` paragraph. These don't affect
	// tag extraction or theme clustering — they exist so the model can
	// ground the insights paragraph in something the reader actually
	// wrote, then cite the source briefly.
	if len(dailyNotes) > 0 {
		b.WriteString("\n## Daily additional notes (free text per day)\n")
		b.WriteString("Each entry is the user's free-text \"additional notes\" for that day.\n")
		b.WriteString("Use these to ground the `insights` paragraph; cite the day briefly.\n\n")
		for _, n := range dailyNotes {
			fmt.Fprintf(&b, "### %s\n  %s\n\n", n.LocalDate, truncateForPrompt(n.Text, 1200))
		}
	}

	if len(manualEntries) > 0 {
		b.WriteString("\n## Manual journal entries this week\n")
		b.WriteString("Each entry is a question the user answered in writing this week.\n")
		b.WriteString("Use these to ground the `insights` paragraph; cite the day or question briefly.\n\n")
		for _, e := range manualEntries {
			fmt.Fprintf(&b, "### %s — %s\n  %s\n\n",
				e.LocalDate, truncateForPrompt(e.Prompt, 200), truncateForPrompt(e.Body, 1500))
		}
	}

	if priorReflection != nil {
		surprise := strings.TrimSpace(priorReflection.SurpriseText)
		if surprise != "" {
			fmt.Fprintf(&b, "\n## Last week's reflection (week of %s)\n", priorReflection.WeekStart)
			b.WriteString("The user wrote this as the response to last week's closing question /\n")
			b.WriteString("surprise prompt. If it matches a pattern in this week's data, reference\n")
			b.WriteString("it briefly in the `insights` paragraph (\"matching what was noticed\n")
			b.WriteString("last week…\").\n\n")
			fmt.Fprintf(&b, "  %s\n", truncateForPrompt(surprise, 1500))
		}
	}

	b.WriteString(
		"\nNow emit the JSON object exactly per the schema in the system " +
			"prompt. Do not include any prose outside the JSON object.")
	return b.String()
}

// truncateForPrompt clips overly-long user text so a runaway journal
// entry can't blow the Gemma context budget. The trailing ellipsis is
// a hint to the model that more existed.
func truncateForPrompt(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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
