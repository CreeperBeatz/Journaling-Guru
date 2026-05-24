package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/cosmosthrace/journai/backend/internal/config"
	"github.com/cosmosthrace/journai/backend/internal/jobs"
	"github.com/cosmosthrace/journai/backend/internal/llm"
	"github.com/cosmosthrace/journai/backend/internal/push"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// Worker binary: River-backed pipeline for summaries, push reminders,
// and chat extraction. Under the Energy Audit pivot the daily/monthly/
// yearly summary branches and the standalone emotion classifier are
// retired — only the weekly summary survives, and emotions are no
// longer extracted as Plutchik subtypes.
//
// Two cooperating loops:
//   - dispatcher tick: every SUMMARY_DISPATCH_INTERVAL_SECONDS, atomically
//     claim due rows from summary_jobs, reminder_jobs, and
//     chat_extraction_jobs, enqueuing a River job per row. Single source
//     of truth for "what should run."
//   - River worker pool: consumes SummaryArgs / ReminderArgs /
//     ChatExtractionArgs, runs the LLM call (or push fan-out), writes
//     the row, and schedules the next slot.
func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config", "err", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	users := store.NewUserStore(db)
	entries := store.NewEntryStore(db)
	dailyInputs := store.NewDailyInputStore(db)
	summaries := store.NewSummaryStore(db)
	jobsStore := store.NewSummaryJobStore(db)
	tagStore := store.NewTagStore(db)
	dailyEntryTags := store.NewDailyEntryTagStore(db)
	pushSubs := store.NewPushSubscriptionStore(db)
	reminderJobs := store.NewReminderJobStore(db)
	questions := store.NewQuestionStore(db)
	chatSessions := store.NewChatSessionStore(db)
	chatMessages := store.NewChatMessageStore(db)
	chatExtractionJobs := store.NewChatExtractionJobStore(db)
	goalStore := store.NewGoalStore(db)
	goalCheckIns := store.NewGoalCheckInStore(db)
	weeklyReflections := store.NewWeeklyReflectionStore(db)

	summaryLLM := llm.NewOpenRouter(
		cfg.OpenRouterKey, cfg.SummaryModel,
		cfg.PublicBaseURL, "Journaling Guru",
	)
	classifyLLM := llm.NewOpenRouter(
		cfg.OpenRouterKey, cfg.ClassifyModel,
		cfg.PublicBaseURL, "Journaling Guru",
	)

	scheduler := &jobs.Scheduler{
		Jobs:           jobsStore,
		Users:          users,
		Logger:         logger,
		InactivityDays: cfg.SummaryInactivityDays,
	}

	worker := &jobs.SummaryWorker{
		Summaries:         summaries,
		Jobs:              jobsStore,
		Entries:           entries,
		DailyInputs:       dailyInputs,
		DailyEntryTags:    dailyEntryTags,
		Tags:              tagStore,
		Users:             users,
		WeeklyReflections: weeklyReflections,
		Scheduler:         scheduler,
		LLM:               summaryLLM,
		Logger:            logger,
		ShotCount:         cfg.SummaryShotCount,
	}
	reminderScheduler := &jobs.ReminderScheduler{
		Jobs:   reminderJobs,
		Users:  users,
		Logger: logger,
	}

	// Push sender — nil-safe: when VAPID keys are missing, the push
	// worker marks rows failed instead of silently no-opping (see
	// PushWorker.Work). The api binary builds its own sender for /test.
	var pushSender push.Sender
	if c, err := push.New(push.Config{
		PublicKey:  cfg.VAPIDPublic,
		PrivateKey: cfg.VAPIDPrivate,
		Subject:    cfg.VAPIDSubject,
	}); err == nil {
		pushSender = c
	} else {
		logger.Warn("push sender disabled — VAPID keys missing", "err", err)
	}

	pushWorker := &jobs.PushWorker{
		Reminders:     reminderJobs,
		Subscriptions: pushSubs,
		Users:         users,
		Sender:        pushSender,
		Scheduler:     reminderScheduler,
		Logger:        logger,
		AppOrigin:     cfg.PublicBaseURL,
	}

	chatExtractWorker := &jobs.ChatExtractionWorker{
		Sessions:       chatSessions,
		Messages:       chatMessages,
		Jobs:           chatExtractionJobs,
		Entries:        entries,
		DailyInputs:    dailyInputs,
		Tags:           tagStore,
		DailyEntryTags: dailyEntryTags,
		Questions:      questions,
		Goals:          goalStore,
		GoalCheckIns:   goalCheckIns,
		Users:          users,
		Scheduler:      scheduler,
		LLM:            classifyLLM,
		Logger:         logger,
	}

	chatIdleSweeper := &jobs.ChatIdleSweeper{
		Sessions:  chatSessions,
		Jobs:      chatExtractionJobs,
		IdleAfter: time.Duration(cfg.ChatIdleTimeoutMinutes) * time.Minute,
		Logger:    logger,
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, worker)
	river.AddWorker(workers, pushWorker)
	river.AddWorker(workers, chatExtractWorker)

	riverClient, err := river.NewClient(riverpgxv5.New(db), &river.Config{
		Queues: map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 4},
		},
		Workers: workers,
		Logger:  logger,
	})
	if err != nil {
		logger.Error("river client", "err", err)
		os.Exit(1)
	}
	if err := riverClient.Start(rootCtx); err != nil {
		logger.Error("river start", "err", err)
		os.Exit(1)
	}
	logger.Info("worker running",
		"env", cfg.AppEnv,
		"dispatch_interval_seconds", cfg.SummaryDispatchInterval,
		"inactivity_days", cfg.SummaryInactivityDays,
		"summary_model", cfg.SummaryModel,
		"summary_shot_count", cfg.SummaryShotCount,
		"classify_model", cfg.ClassifyModel,
		"openrouter_key_set", cfg.OpenRouterKey != "",
		"push_sender_set", pushSender != nil,
	)

	go runDispatchLoop(rootCtx, logger, jobsStore, reminderJobs,
		chatExtractionJobs, chatIdleSweeper,
		riverClient, time.Duration(cfg.SummaryDispatchInterval)*time.Second)

	<-rootCtx.Done()
	logger.Info("worker shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := riverClient.Stop(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("river stop", "err", err)
	}
}

// runDispatchLoop is the every-N-seconds tick that drains the surviving
// three job queues (summary, reminder, chat extraction) and runs the
// chat idle sweeper. Atomic claim (FOR UPDATE SKIP LOCKED) means
// concurrent worker replicas can't double-enqueue.
func runDispatchLoop(
	ctx context.Context,
	logger *slog.Logger,
	jobsStore *store.SummaryJobStore,
	reminderJobs *store.ReminderJobStore,
	chatExtractionJobs *store.ChatExtractionJobStore,
	chatIdleSweeper *jobs.ChatIdleSweeper,
	riverClient *river.Client[pgx.Tx],
	interval time.Duration,
) {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately so a freshly-restarted worker doesn't sit
	// idle for a full interval if there's a backlog.
	tick(ctx, logger, jobsStore, reminderJobs, chatExtractionJobs, chatIdleSweeper, riverClient)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick(ctx, logger, jobsStore, reminderJobs, chatExtractionJobs, chatIdleSweeper, riverClient)
		}
	}
}

func tick(
	ctx context.Context,
	logger *slog.Logger,
	jobsStore *store.SummaryJobStore,
	reminderJobs *store.ReminderJobStore,
	chatExtractionJobs *store.ChatExtractionJobStore,
	chatIdleSweeper *jobs.ChatIdleSweeper,
	riverClient *river.Client[pgx.Tx],
) {
	const batch = 100

	claimed, err := jobsStore.ClaimDue(ctx, batch)
	if err != nil {
		logger.Warn("claim due summaries", "err", err)
	} else if len(claimed) > 0 {
		logger.Info("dispatching summary jobs", "count", len(claimed))
		for _, c := range claimed {
			if _, err := riverClient.Insert(ctx, jobs.SummaryArgs{JobID: c.ID}, nil); err != nil {
				logger.Warn("river insert (summary)", "err", err, "job_id", c.ID)
				_ = jobsStore.ReleaseForRetry(ctx, c.ID, "river insert: "+err.Error())
			}
		}
	}

	claimedReminders, err := reminderJobs.ClaimDue(ctx, batch)
	if err != nil {
		logger.Warn("claim due reminders", "err", err)
	} else if len(claimedReminders) > 0 {
		logger.Info("dispatching reminder jobs", "count", len(claimedReminders))
		for _, c := range claimedReminders {
			if _, err := riverClient.Insert(ctx, jobs.ReminderArgs{JobID: c.ID}, nil); err != nil {
				logger.Warn("river insert (reminder)", "err", err, "job_id", c.ID)
				_ = reminderJobs.ReleaseForRetry(ctx, c.ID, "river insert: "+err.Error())
			}
		}
	}

	// Chat idle sweeper writes chat_extraction_jobs rows for stale
	// sessions; the next claim block picks them up.
	if chatIdleSweeper != nil {
		chatIdleSweeper.Sweep(ctx, batch)
	}

	claimedChat, err := chatExtractionJobs.ClaimDue(ctx, batch)
	if err != nil {
		logger.Warn("claim due chat extractions", "err", err)
		return
	}
	if len(claimedChat) == 0 {
		return
	}
	logger.Info("dispatching chat extraction jobs", "count", len(claimedChat))
	for _, c := range claimedChat {
		if _, err := riverClient.Insert(ctx, jobs.ChatExtractionArgs{JobID: c.ID}, nil); err != nil {
			logger.Warn("river insert (chat extraction)", "err", err, "job_id", c.ID)
			_ = chatExtractionJobs.ReleaseForRetry(ctx, c.ID, "river insert: "+err.Error())
		}
	}
}
