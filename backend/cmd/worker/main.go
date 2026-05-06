package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/config"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// Phase 1 stub. Phase 4 wires River and registers daily/weekly/monthly/yearly
// summary jobs; Phase 5 adds the push-dispatch tick. For now the binary just
// blocks so overmind doesn't keep restarting it.
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

	logger.Info("worker idle (Phase 1 stub)", "env", cfg.AppEnv)

	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-rootCtx.Done():
			logger.Info("worker shutting down")
			return
		case <-t.C:
			logger.Debug("worker tick")
		}
	}
}
