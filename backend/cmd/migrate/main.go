package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver with database/sql
	"github.com/pressly/goose/v3"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/cosmosthrace/journai/backend/internal/config"
	"github.com/cosmosthrace/journai/backend/internal/store/migrations"
)

// Usage:
//
//	migrate up           # apply all pending (goose + river)
//	migrate down         # roll back one (goose only — river migrations are stable)
//	migrate status       # list applied
//	migrate redo         # down + up the latest
//
// We run two migration sources in sequence: goose for our domain schema
// (versioned in store/migrations/*.sql), then rivermigrate for River's
// internal queue tables. River's tables live in the same database but a
// separate version space, so they don't collide.
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

	ctx := context.Background()

	if err := runGoose(ctx, cfg.DatabaseURL, cmd); err != nil {
		fail(err)
	}

	// River migrations only make sense in the up direction. For 'down' /
	// 'status' / 'redo' we focus on the goose layer, which owns the data.
	if cmd == "up" {
		if err := runRiver(ctx, cfg.DatabaseURL); err != nil {
			fail(fmt.Errorf("river migrate: %w", err))
		}
	}
}

func runGoose(ctx context.Context, dsn, cmd string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping db: %w", err)
	}

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	if err := goose.RunContext(ctx, cmd, db, "."); err != nil {
		if errors.Is(err, goose.ErrNoNextVersion) || errors.Is(err, goose.ErrNoCurrentVersion) {
			slog.Info("goose: nothing to do", "cmd", cmd)
			return nil
		}
		return err
	}
	return nil
}

func runRiver(ctx context.Context, dsn string) error {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	res, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil)
	if err != nil {
		return err
	}
	for _, v := range res.Versions {
		slog.Info("river migration applied", "version", v.Version, "name", v.Name)
	}
	if len(res.Versions) == 0 {
		slog.Info("river: nothing to do")
	}
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "migrate:", err)
	os.Exit(1)
}
