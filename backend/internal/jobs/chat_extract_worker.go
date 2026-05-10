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
	"github.com/cosmosthrace/journai/backend/internal/llm/chat"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

type dailyField int

const (
	fieldDrained dailyField = iota
	fieldCharged
	fieldGratitude
	fieldReflection
)

// mergedField returns the value to persist for one daily_input text
// field. If the extracted value is empty, returns "" so the
// session-wins SQL preserves the existing field. If both manual and
// extracted are non-empty, runs an LLM-merge (with append fallback);
// the result replaces both via OverwriteFromExtraction.
func mergedField(
	ctx context.Context,
	client *llm.OpenRouter,
	model, label string,
	existing *domain.DailyInput,
	chatText string,
	field dailyField,
) string {
	chatText = strings.TrimSpace(chatText)
	if chatText == "" {
		return ""
	}
	if existing == nil {
		return chatText
	}
	var existingText string
	switch field {
	case fieldDrained:
		existingText = existing.DrainedText
	case fieldCharged:
		existingText = existing.ChargedText
	case fieldGratitude:
		existingText = existing.GratitudeText
	case fieldReflection:
		existingText = existing.ReflectionText
	}
	existingText = strings.TrimSpace(existingText)
	if existingText == "" {
		return chatText
	}
	return chat.MergeText(ctx, client, model, label, existingText, chatText)
}

// ChatExtractionWorker runs the single-shot extraction LLM call at
// session-end (or idle-finalize) and writes the structured fields to
// daily_inputs + journal_entries.
//
// Idempotency model: if the session is already 'finalized', ack and
// return. Postgres + UNIQUE-constraint guarantees do the rest — manual
// edits always win because we use UpsertIfAbsent for entries and
// MergeFromExtraction for daily_inputs.
type ChatExtractionWorker struct {
	river.WorkerDefaults[ChatExtractionArgs]

	Sessions       *store.ChatSessionStore
	Messages       *store.ChatMessageStore
	Jobs           *store.ChatExtractionJobStore
	Entries        *store.EntryStore
	DailyInputs    *store.DailyInputStore
	Tags           *store.TagStore
	DailyEntryTags *store.DailyEntryTagStore
	Questions      *store.QuestionStore
	Goals          *store.GoalStore
	GoalCheckIns   *store.GoalCheckInStore
	Users          *store.UserStore
	Scheduler      *Scheduler // re-seeds summaries after extraction lands
	// LLM is the classify-tier client (CLASSIFY_MODEL default). Per-call
	// model override comes from the session pin only — no env-level
	// override beyond what the client constructor pinned.
	LLM    *llm.OpenRouter
	Logger *slog.Logger
}

// Work is River's entrypoint. Mirrors the SummaryWorker error-encoding
// pattern: errors get persisted into chat_extraction_jobs.last_error so
// a re-claim has context, and the final attempt marks failed instead of
// bubbling to River (which would otherwise land the row in 'discarded'
// state with no journai-side trace).
func (w *ChatExtractionWorker) Work(ctx context.Context, rj *river.Job[ChatExtractionArgs]) error {
	job, err := w.Jobs.GetByID(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, store.ErrChatExtractionJobNotFound) {
			// Race with delete cascade (user deleted account, or session
			// dropped). Don't retry.
			return nil
		}
		return err
	}
	switch job.Status {
	case "completed", "skipped", "failed":
		// Terminal — a stale River retry can land here.
		return nil
	}

	session, err := w.Sessions.GetByIDForWorker(ctx, job.SessionID)
	if err != nil {
		if errors.Is(err, store.ErrChatSessionNotFound) {
			return w.Jobs.MarkSkipped(ctx, job.ID)
		}
		return err
	}
	// Idempotency comes from the chat_extraction_jobs row's status
	// (terminal statuses early-return at the top of Work). The session
	// phase is informational only in the open-chat model; running
	// extraction again on a previously-finalized session is the user's
	// "Update check-in" click after continuing the conversation.

	user, err := w.Users.GetByID(ctx, session.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		// User soft-deleted between scheduling and firing.
		_ = w.Sessions.SetExtractionStatus(ctx, session.ID, domain.ChatExtractionFailed,
			ptrString("user not found"))
		return w.Jobs.MarkSkipped(ctx, job.ID)
	}

	if err := w.process(ctx, job, session, user); err != nil {
		isFinal := rj.Attempt >= rj.MaxAttempts
		w.Logger.Warn("chat extraction error",
			"err", err,
			"job_id", job.ID,
			"session_id", session.ID,
			"attempt", rj.Attempt,
			"max", rj.MaxAttempts,
		)
		if isFinal {
			_ = w.Sessions.SetExtractionStatus(ctx, session.ID, domain.ChatExtractionFailed, ptrString(err.Error()))
			_ = w.Jobs.MarkFailed(ctx, job.ID, err.Error())
			return nil
		}
		_ = w.Jobs.ReleaseForRetry(ctx, job.ID, err.Error())
		return err
	}
	return nil
}

// process is the inner extraction flow, separated so Work can do the
// error-encoding wrapper without nesting.
func (w *ChatExtractionWorker) process(
	ctx context.Context,
	job *domain.ChatExtractionJob,
	session *domain.ChatSession,
	user *domain.User,
) error {
	// Mark running so the polling endpoint reflects progress.
	if err := w.Sessions.SetExtractionStatus(ctx, session.ID, domain.ChatExtractionRunning, nil); err != nil {
		w.Logger.Warn("set extraction running", "err", err, "session_id", session.ID)
	}

	messages, err := w.Messages.ListBySession(ctx, session.ID)
	if err != nil {
		return fmt.Errorf("load messages: %w", err)
	}
	// Empty / opener-only transcript → skip extraction; advance phase to
	// abandoned so the row stops appearing in the idle sweeper. Manual
	// fields stay untouched.
	if !hasUsableTranscript(messages) {
		_, _ = w.Sessions.AdvancePhase(ctx, session.ID, domain.ChatPhaseAbandoned)
		_ = w.Sessions.SetExtractionStatus(ctx, session.ID, domain.ChatExtractionCompleted, nil)
		return w.Jobs.MarkSkipped(ctx, job.ID)
	}

	questions, err := w.Questions.ListActive(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("load questions: %w", err)
	}
	views := chat.QuestionViewsFromDomain(questions)

	// Active goals for the journal date. ListActive already filters by
	// status='active' AND end_date >= asOf; ordered by end_date ASC so
	// the prompt surfaces near-end goals first.
	var goalViews []chat.GoalView
	if w.Goals != nil {
		asOf, perr := time.Parse("2006-01-02", session.LocalDate)
		if perr != nil {
			return fmt.Errorf("parse session local_date: %w", perr)
		}
		activeGoals, gerr := w.Goals.ListActive(ctx, user.ID, asOf)
		if gerr != nil {
			return fmt.Errorf("load active goals: %w", gerr)
		}
		goalViews = chat.GoalViewsFromDomain(activeGoals)
	}

	// Per-session pin (set on CreateOrResume) wins over the LLM client
	// default — keeps replays consistent across env changes. Empty pin
	// → empty per-call Model → client default (CLASSIFY_MODEL).
	model := session.ExtractionModel

	result, err := chat.Extract(ctx, w.LLM, chat.ExtractParams{
		Model:     model,
		Questions: views,
		Goals:     goalViews,
		Messages:  messages,
	})
	if err != nil {
		return err
	}

	if err := ApplyExtraction(ctx, ApplyDeps{
		Sessions:       w.Sessions,
		Entries:        w.Entries,
		DailyInputs:    w.DailyInputs,
		Tags:           w.Tags,
		DailyEntryTags: w.DailyEntryTags,
		Goals:          w.Goals,
		GoalCheckIns:   w.GoalCheckIns,
		LLM:            w.LLM,
		Logger:         w.Logger,
		Scheduler:      w.Scheduler,
	}, result, user, session, views, model); err != nil {
		return err
	}

	return w.Jobs.MarkCompleted(ctx, job.ID)
}

// hasUsableTranscript decides whether a transcript is substantial enough
// to feed to the extractor. We require at least one user turn — opener-
// only sessions have just the assistant greeting, which has nothing to
// extract from.
func hasUsableTranscript(messages []domain.ChatMessage) bool {
	for _, m := range messages {
		if m.Role == domain.ChatRoleUser && len(m.Content) > 0 {
			return true
		}
	}
	return false
}

func ptrString(s string) *string { return &s }
