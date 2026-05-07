package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// emotionClassifyMaxTokens is the response cap. Six entries × ~30 tokens
// each + structural overhead leaves headroom; tight enough to keep cost
// bounded if the model goes off-script.
const emotionClassifyMaxTokens = 400

// emotionClassifyMaxEntries caps post-parse output. Even if the LLM
// emits more than the prompt allows, we trim — keeps SummaryDetail's
// pill row reasonable.
const emotionClassifyMaxEntries = 6

// emotionRawPhraseMaxLen clips the raw_phrase echo so a verbose
// hallucination can't bloat the row. 120 chars > most naturally sliced
// phrases.
const emotionRawPhraseMaxLen = 120

// EmotionClassifyWorker turns daily_inputs.emotions_text into structured
// (base, subtype, raw_phrase) entries via OpenRouter and writes them to
// daily_inputs.classified_emotions. Empty text → MarkSkipped without
// calling the LLM. Validation: any entry whose (base, subtype) is not in
// domain.PlutchikSubtypes is dropped.
type EmotionClassifyWorker struct {
	river.WorkerDefaults[EmotionClassifyArgs]

	DailyInputs *store.DailyInputStore
	EmotionJobs *store.EmotionClassifyJobStore
	Users       *store.UserStore
	LLM         *llm.OpenRouter
	Logger      *slog.Logger
}

// emotionClassifyResponse is the schema enforced via prompt.
type emotionClassifyResponse struct {
	Emotions []domain.ClassifiedEmotion `json:"emotions"`
}

// Work is River's entrypoint. Mirrors SummaryWorker.Work's error-encoding
// pattern: errors get persisted into emotion_classify_jobs.last_error so
// a re-claim has context, and the final attempt marks failed instead of
// bubbling out (which would otherwise land the row in River's discarded
// state with no journai-side trace).
func (w *EmotionClassifyWorker) Work(ctx context.Context, rj *river.Job[EmotionClassifyArgs]) error {
	job, err := w.EmotionJobs.GetByID(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, store.ErrEmotionClassifyJobNotFound) {
			return nil
		}
		return err
	}
	switch job.Status {
	case "completed", "skipped", "failed":
		return nil
	}

	user, err := w.Users.GetByID(ctx, job.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		return w.EmotionJobs.MarkSkipped(ctx, job.ID)
	}

	if err := w.process(ctx, job); err != nil {
		isFinal := rj.Attempt >= rj.MaxAttempts
		w.Logger.Warn("emotion classify error",
			"err", err,
			"job_id", job.ID,
			"local_date", job.LocalDate,
			"attempt", rj.Attempt,
			"max", rj.MaxAttempts,
		)
		if isFinal {
			_ = w.EmotionJobs.MarkFailed(ctx, job.ID, err.Error())
			return nil
		}
		_ = w.EmotionJobs.ReleaseForRetry(ctx, job.ID, err.Error())
		return err
	}
	return nil
}

func (w *EmotionClassifyWorker) process(ctx context.Context, job *domain.EmotionClassifyJob) error {
	localDate, err := time.Parse("2006-01-02", job.LocalDate)
	if err != nil {
		return fmt.Errorf("parse local_date: %w", err)
	}

	checkin, err := w.DailyInputs.GetByDate(ctx, job.UserID, localDate)
	if err != nil {
		return fmt.Errorf("load daily input: %w", err)
	}
	// Row vanished (empty-deletes) or text cleared — nothing to classify.
	// Reset classified_emotions to [] so stale pills don't linger and
	// mark the job skipped.
	if checkin == nil || strings.TrimSpace(checkin.EmotionsText) == "" {
		if checkin != nil {
			if err := w.DailyInputs.WriteClassifiedEmotions(ctx, job.UserID, localDate, nil); err != nil {
				return fmt.Errorf("clear classified_emotions: %w", err)
			}
		}
		return w.EmotionJobs.MarkSkipped(ctx, job.ID)
	}

	userPrompt, err := renderTemplate("emotions.tmpl", map[string]any{
		"Date":         job.LocalDate,
		"EmotionsText": strings.TrimSpace(checkin.EmotionsText),
	})
	if err != nil {
		return err
	}
	resp, err := w.LLM.Complete(ctx, llm.CompletionRequest{
		System:    emotionClassifySystemPrompt,
		User:      userPrompt,
		MaxTokens: emotionClassifyMaxTokens,
		JSONMode:  true,
	})
	if err != nil {
		return err
	}
	parsed, err := parseEmotionClassifyJSON(resp.Content)
	if err != nil {
		return fmt.Errorf("parse classifier response: %w (content: %s)", err, truncate(resp.Content, 300))
	}
	classified := normalizeClassified(parsed.Emotions)

	if err := w.DailyInputs.WriteClassifiedEmotions(ctx, job.UserID, localDate, classified); err != nil {
		return fmt.Errorf("write classified_emotions: %w", err)
	}
	return w.EmotionJobs.MarkCompleted(ctx, job.ID)
}

// parseEmotionClassifyJSON tolerates the same ```json ... ``` fences
// stripFences handles for the daily-summary parser.
func parseEmotionClassifyJSON(content string) (*emotionClassifyResponse, error) {
	cleaned := stripFences(content)
	var out emotionClassifyResponse
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, err
	}
	if out.Emotions == nil {
		out.Emotions = []domain.ClassifiedEmotion{}
	}
	return &out, nil
}

// normalizeClassified validates every entry against PlutchikSubtypes and
// drops invalid ones, lowercases base/subtype, trims raw_phrase, and
// caps at emotionClassifyMaxEntries.
func normalizeClassified(in []domain.ClassifiedEmotion) []domain.ClassifiedEmotion {
	out := make([]domain.ClassifiedEmotion, 0, len(in))
	seen := map[string]struct{}{}
	for _, e := range in {
		base := strings.ToLower(strings.TrimSpace(e.Base))
		subtype := strings.ToLower(strings.TrimSpace(e.Subtype))
		if !domain.IsValidPlutchik(base, subtype) {
			continue
		}
		// Same subtype twice → keep first (most-salient-first ordering).
		if _, ok := seen[subtype]; ok {
			continue
		}
		seen[subtype] = struct{}{}
		raw := strings.TrimSpace(e.RawPhrase)
		if len(raw) > emotionRawPhraseMaxLen {
			raw = raw[:emotionRawPhraseMaxLen] + "…"
		}
		out = append(out, domain.ClassifiedEmotion{
			Base:      base,
			Subtype:   subtype,
			RawPhrase: raw,
		})
		if len(out) >= emotionClassifyMaxEntries {
			break
		}
	}
	return out
}
