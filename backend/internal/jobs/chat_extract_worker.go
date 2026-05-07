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

	Sessions    *store.ChatSessionStore
	Messages    *store.ChatMessageStore
	Jobs        *store.ChatExtractionJobStore
	Entries     *store.EntryStore
	DailyInputs *store.DailyInputStore
	Questions   *store.QuestionStore
	Users       *store.UserStore
	EmotionJobs *store.EmotionClassifyJobStore
	Scheduler   *Scheduler // re-seeds summaries after extraction lands
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

	// Per-session pin (set on CreateOrResume) wins over the LLM client
	// default — keeps replays consistent across env changes. Empty pin
	// → empty per-call Model → client default (CLASSIFY_MODEL).
	model := session.ExtractionModel

	result, err := chat.Extract(ctx, w.LLM, chat.ExtractParams{
		Model:     model,
		Questions: views,
		Messages:  messages,
	})
	if err != nil {
		return err
	}

	localDate, err := time.Parse("2006-01-02", session.LocalDate)
	if err != nil {
		return fmt.Errorf("parse local_date: %w", err)
	}

	// Overwrite daily_inputs from the extraction. The FE warns the user
	// before triggering finalize, so they consent to losing prior
	// manual values for the fields the chat actually covered. Empty
	// extracted fields preserve existing manual values (so a chat that
	// only covered mood doesn't blank manual notes).
	if _, err := w.DailyInputs.OverwriteFromExtraction(
		ctx, user.ID, localDate,
		result.MoodScore, result.EmotionsToText(), result.Notes,
	); err != nil {
		return fmt.Errorf("overwrite daily_inputs: %w", err)
	}

	// Overwrite each answered question. Same warning-then-overwrite
	// contract as daily_inputs. UpsertFromChat preserves any question
	// the chat didn't actually cover (extraction omits the key).
	for qid, body := range result.Answers {
		_, _, err := w.Entries.UpsertFromChat(
			ctx, user.ID, qid, localDate, body, session.ID,
		)
		if err != nil {
			if errors.Is(err, store.ErrEntryQuestionMissing) {
				// Question was archived between session start and now —
				// silently skip; no point failing the whole extraction.
				w.Logger.Info("skip archived question", "session_id", session.ID, "question_id", qid)
				continue
			}
			return fmt.Errorf("upsert entry %s: %w", qid, err)
		}
	}

	// Arm the Plutchik classifier if we wrote new emotions text. The
	// daily-inputs handler does the same on user PUTs; we mirror the
	// pattern so both sources fan out identically.
	if result.EmotionsToText() != "" {
		if _, err := w.EmotionJobs.Schedule(ctx, user.ID, localDate, time.Now()); err != nil {
			w.Logger.Warn("schedule emotion classify (post-extraction)",
				"err", err, "session_id", session.ID, "user_id", user.ID)
		}
	}

	// Lazy-seed summaries — same pattern as the daily-inputs / entries
	// handlers. The daily summary will be due on the user's next
	// (day_start + 30min) tick.
	if w.Scheduler != nil {
		if err := w.Scheduler.LazySeed(ctx, user.ID, time.Now()); err != nil {
			w.Logger.Warn("lazy seed (chat extraction)",
				"err", err, "session_id", session.ID)
		}
	}

	// Stamp finalized_at as "last extraction completed at" — purely
	// informational; the chat is NOT closed. Then roll the phase back
	// to exploring so the user can keep talking, and the FE composer
	// stays enabled.
	if err := w.Sessions.MarkFinalized(ctx, session.ID); err != nil {
		w.Logger.Warn("mark finalized timestamp", "err", err, "session_id", session.ID)
	}
	if _, err := w.Sessions.AdvancePhase(ctx, session.ID, domain.ChatPhaseExploring); err != nil {
		// Already in exploring (a new user turn beat us to it) is
		// idempotent; other transition errors are unexpected but
		// non-fatal — extraction itself succeeded.
		if !errors.Is(err, store.ErrChatSessionInvalidPhase) {
			w.Logger.Warn("post-extraction phase advance", "err", err, "session_id", session.ID)
		}
	}
	if err := w.Sessions.SetExtractionStatus(ctx, session.ID, domain.ChatExtractionCompleted, nil); err != nil {
		w.Logger.Warn("set extraction completed", "err", err, "session_id", session.ID)
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
