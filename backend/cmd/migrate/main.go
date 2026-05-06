package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver with database/sql
	"github.com/pressly/goose/v3"

	"github.com/cosmosthrace/journai/backend/internal/config"
	"github.com/cosmosthrace/journai/backend/internal/store/migrations"
)

// Usage:
//   migrate up           # apply all pending
//   migrate down         # roll back one
//   migrate status       # list applied
//   migrate redo         # down + up the latest
func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		args = []string{"up"}
	}
	cmd := args[0]

	cfg, err := config.Load()
	if err != nil {
		fail(err)
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	if err := db.PingContext(context.Background()); err != nil {
		fail(fmt.Errorf("ping db: %w", err))
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		fail(err)
	}

	if err := goose.RunContext(context.Background(), cmd, db, "."); err != nil {
		if errors.Is(err, goose.ErrNoNextVersion) || errors.Is(err, goose.ErrNoCurrentVersion) {
			slog.Info("goose: nothing to do", "cmd", cmd)
			return
		}
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "migrate:", err)
	os.Exit(1)
}
