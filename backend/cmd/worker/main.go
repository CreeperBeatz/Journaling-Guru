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
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// Phase 4 worker: River-backed summary pipeline + Phase 4.1 emotion
// classifier.
//
// Two cooperating loops:
//   - dispatcher tick: every SUMMARY_DISPATCH_INTERVAL_SECONDS, atomically
//     claim due rows from BOTH summary_jobs and emotion_classify_jobs and
//     enqueue a River job per row. Single source of truth for "what
//     should run."
//   - River worker pool: consumes SummaryArgs / EmotionClassifyArgs,
//     runs the LLM call, writes the row, and (for summaries) schedules
//     the next period.
//
// Phase 5 will add a push-dispatch tick alongside this one.
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
	emotionJobs := store.NewEmotionClassifyJobStore(db)

	llmClient := llm.NewOpenRouter(
		cfg.OpenRouterKey, cfg.OpenRouterModel,
		cfg.PublicBaseURL, "JournAI",
	)

	scheduler := &jobs.Scheduler{
		Jobs:           jobsStore,
		Users:          users,
		Logger:         logger,
		InactivityDays: cfg.SummaryInactivityDays,
	}

	worker := &jobs.SummaryWorker{
		Summaries:   summaries,
		Jobs:        jobsStore,
		Entries:     entries,
		DailyInputs: dailyInputs,
		Users:       users,
		Scheduler:   scheduler,
		LLM:         llmClient,
		Logger:      logger,
	}
	emotionWorker := &jobs.EmotionClassifyWorker{
		DailyInputs: dailyInputs,
		EmotionJobs: emotionJobs,
		Users:       users,
		LLM:         llmClient,
		Logger:      logger,
	}

	workers := river.NewWorkers()
	river.AddWorker(workers, worker)
	river.AddWorker(workers, emotionWorker)

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
		"openrouter_model", cfg.OpenRouterModel,
		"openrouter_key_set", cfg.OpenRouterKey != "",
	)

	go runDispatchLoop(rootCtx, logger, jobsStore, emotionJobs, riverClient, time.Duration(cfg.SummaryDispatchInterval)*time.Second)

	<-rootCtx.Done()
	logger.Info("worker shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := riverClient.Stop(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("river stop", "err", err)
	}
}

// runDispatchLoop is the every-N-seconds tick that drains both job
// queues. Atomic claim (FOR UPDATE SKIP LOCKED) means concurrent worker
// replicas can't double-enqueue.
func runDispatchLoop(
	ctx context.Context,
	logger *slog.Logger,
	jobsStore *store.SummaryJobStore,
	emotionJobs *store.EmotionClassifyJobStore,
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
	tick(ctx, logger, jobsStore, emotionJobs, riverClient)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tick(ctx, logger, jobsStore, emotionJobs, riverClient)
		}
	}
}

func tick(
	ctx context.Context,
	logger *slog.Logger,
	jobsStore *store.SummaryJobStore,
	emotionJobs *store.EmotionClassifyJobStore,
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

	claimedEmotions, err := emotionJobs.ClaimDue(ctx, batch)
	if err != nil {
		logger.Warn("claim due emotions", "err", err)
		return
	}
	if len(claimedEmotions) == 0 {
		return
	}
	logger.Info("dispatching emotion classify jobs", "count", len(claimedEmotions))
	for _, c := range claimedEmotions {
		if _, err := riverClient.Insert(ctx, jobs.EmotionClassifyArgs{JobID: c.ID}, nil); err != nil {
			logger.Warn("river insert (emotion)", "err", err, "job_id", c.ID)
			_ = emotionJobs.ReleaseForRetry(ctx, c.ID, "river insert: "+err.Error())
		}
	}
}
