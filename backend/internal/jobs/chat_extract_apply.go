package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/llm/chat"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// ApplyDeps bundles the stores + LLM client + logger needed to apply an
// extraction result to the database. Used by the async chat-extraction
// worker. Merge semantics: LLM-merge for non-empty conflicts, otherwise
// insert.
type ApplyDeps struct {
	Sessions       *store.ChatSessionStore
	Entries        *store.EntryStore
	DailyInputs    *store.DailyInputStore
	Tags           *store.TagStore
	DailyEntryTags *store.DailyEntryTagStore
	Goals          *store.GoalStore        // optional; nil disables goal_check_ins writes
	GoalCheckIns   *store.GoalCheckInStore // paired with Goals
	LLM            *llm.OpenRouter
	Logger         *slog.Logger
	Scheduler      *Scheduler // optional; nil-safe
}

// ApplyExtraction writes the extraction result to daily_inputs +
// journal_entries with manual-wins / LLM-merge semantics. Caller is
// responsible for: (1) running chat.Extract to produce `result`,
// (2) phase + extraction_status bookkeeping outside this function. We
// stamp finalized_at + advance phase back to exploring + lazy-seed
// summaries here because every caller wants that.
func ApplyExtraction(
	ctx context.Context,
	d ApplyDeps,
	result *chat.ExtractionResult,
	user *domain.User,
	session *domain.ChatSession,
	views []chat.QuestionView,
	mergeModel string,
) error {
	localDate, err := time.Parse("2006-01-02", session.LocalDate)
	if err != nil {
		return fmt.Errorf("parse local_date: %w", err)
	}

	existingDaily, err := d.DailyInputs.GetByDate(ctx, user.ID, localDate)
	if err != nil {
		return fmt.Errorf("load existing daily_inputs: %w", err)
	}
	dailyPayload := store.DailyInputUpsert{
		Mood:           result.Mood,
		DrainedText:    mergedField(ctx, d.LLM, mergeModel, "what drained you today", existingDaily, result.DrainedText, fieldDrained),
		ChargedText:    mergedField(ctx, d.LLM, mergeModel, "what energized/charged you today", existingDaily, result.ChargedText, fieldCharged),
		GratitudeText:  mergedField(ctx, d.LLM, mergeModel, "what you're grateful for today", existingDaily, result.GratitudeText, fieldGratitude),
		ReflectionText: mergedField(ctx, d.LLM, mergeModel, "broader reflection on today", existingDaily, result.ReflectionText, fieldReflection),
	}
	if _, err := d.DailyInputs.OverwriteFromExtraction(ctx, user.ID, localDate, dailyPayload); err != nil {
		return fmt.Errorf("write daily_inputs: %w", err)
	}

	if d.Tags != nil && d.DailyEntryTags != nil {
		drainerIDs, err := upsertTagBatch(ctx, d.Tags, user.ID, result.DrainedTagProposals, "negative")
		if err != nil {
			return fmt.Errorf("upsert drainer tags: %w", err)
		}
		chargerIDs, err := upsertTagBatch(ctx, d.Tags, user.ID, result.ChargedTagProposals, "positive")
		if err != nil {
			return fmt.Errorf("upsert charger tags: %w", err)
		}
		if len(drainerIDs) > 0 {
			if err := d.DailyEntryTags.ReplaceForDay(ctx, user.ID, localDate, "drainer", drainerIDs); err != nil {
				return fmt.Errorf("link drainer tags: %w", err)
			}
		}
		if len(chargerIDs) > 0 {
			if err := d.DailyEntryTags.ReplaceForDay(ctx, user.ID, localDate, "charger", chargerIDs); err != nil {
				return fmt.Errorf("link charger tags: %w", err)
			}
		}
	}

	for qid, body := range result.Answers {
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		existing, err := d.Entries.GetByQuestionAndDate(ctx, user.ID, qid, localDate)
		if err != nil {
			return fmt.Errorf("load existing entry %s: %w", qid, err)
		}
		prompt := ""
		for _, q := range views {
			if q.ID == qid {
				prompt = q.Prompt
				break
			}
		}
		if existing == nil || strings.TrimSpace(existing.Body) == "" {
			if _, _, err := d.Entries.UpsertFromChat(ctx, user.ID, qid, localDate, body, session.ID); err != nil {
				if errors.Is(err, store.ErrEntryQuestionMissing) {
					if d.Logger != nil {
						d.Logger.Info("skip archived question", "session_id", session.ID, "question_id", qid)
					}
					continue
				}
				return fmt.Errorf("upsert entry %s: %w", qid, err)
			}
			continue
		}
		merged := chat.MergeEntry(ctx, d.LLM, mergeModel, prompt, existing.Body, body)
		if strings.TrimSpace(merged) == strings.TrimSpace(existing.Body) {
			continue
		}
		if _, _, err := d.Entries.UpdateBody(ctx, user.ID, existing.ID, merged); err != nil {
			if errors.Is(err, store.ErrEntryNotFound) {
				continue
			}
			return fmt.Errorf("update entry %s: %w", qid, err)
		}
	}

	if d.Goals != nil && d.GoalCheckIns != nil && len(result.GoalCheckIns) > 0 {
		existingCheckIns, err := d.GoalCheckIns.GetForDay(ctx, user.ID, localDate)
		if err != nil {
			return fmt.Errorf("load existing goal check-ins: %w", err)
		}
		for _, gc := range result.GoalCheckIns {
			// Manual-wins: if the user already toggled the check-in
			// on /today before extraction ran, leave it alone.
			if _, exists := existingCheckIns[gc.GoalID]; exists {
				continue
			}
			// Defensive: re-fetch the goal. validateExtraction only
			// asserted the ID was in the listed goals; status and
			// date range can still drift between list and apply.
			g, err := d.Goals.GetByID(ctx, user.ID, gc.GoalID)
			if err != nil {
				return fmt.Errorf("load goal %s: %w", gc.GoalID, err)
			}
			if g == nil || g.Status != domain.GoalStatusActive {
				continue
			}
			if session.LocalDate < g.StartDate || session.LocalDate > g.EndDate {
				continue
			}
			if _, err := d.GoalCheckIns.Upsert(ctx, gc.GoalID, localDate, gc.Value); err != nil {
				return fmt.Errorf("upsert goal check-in %s: %w", gc.GoalID, err)
			}
		}
	}

	if d.Scheduler != nil {
		if err := d.Scheduler.LazySeed(ctx, user.ID, time.Now()); err != nil && d.Logger != nil {
			d.Logger.Warn("lazy seed (chat extraction)", "err", err, "session_id", session.ID)
		}
	}

	if err := d.Sessions.MarkFinalized(ctx, session.ID); err != nil && d.Logger != nil {
		d.Logger.Warn("mark finalized timestamp", "err", err, "session_id", session.ID)
	}
	if _, err := d.Sessions.AdvancePhase(ctx, session.ID, domain.ChatPhaseExploring); err != nil {
		if !errors.Is(err, store.ErrChatSessionInvalidPhase) && d.Logger != nil {
			d.Logger.Warn("post-extraction phase advance", "err", err, "session_id", session.ID)
		}
	}
	if err := d.Sessions.SetExtractionStatus(ctx, session.ID, domain.ChatExtractionCompleted, nil); err != nil && d.Logger != nil {
		d.Logger.Warn("set extraction completed", "err", err, "session_id", session.ID)
	}
	return nil
}

func upsertTagBatch(
	ctx context.Context, tags *store.TagStore, userID string, labels []string, valence string,
) ([]string, error) {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		t, err := tags.UpsertByLabel(ctx, userID, label, valence)
		if err != nil {
			return nil, err
		}
		out = append(out, t.ID)
	}
	return out, nil
}
