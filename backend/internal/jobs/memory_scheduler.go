package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/domain"
	"github.com/cosmosthrace/journai/backend/internal/store"
	"github.com/cosmosthrace/journai/backend/internal/timezone"
)

// MemoryScheduler owns "what memory_extraction_jobs row should exist and
// when" — the memory analogue of Scheduler. One row per (user,
// local_date), firing at day close (next day at day_start+30 in user tz,
// the instant the daily summary used to fire) so the reconciliation pass
// sees the finished day.
//
// No dormancy guard, deliberately: memory jobs only exist for days the
// user actually wrote something (lazy-seeded from the write path), so
// there is no idle re-arm loop to pause — and a returning user's first
// day back should produce memories.
type MemoryScheduler struct {
	Jobs   *store.MemoryExtractionJobStore
	Users  *store.UserStore
	Logger *slog.Logger
}

// LazySeed arms the memory job for the user-local day containing `at`.
// Idempotent via ON CONFLICT DO NOTHING — called on every entry /
// daily-input write and after chat extraction lands.
//
// Note AllPeriods is not used here: under the Energy Audit pivot it only
// emits the week period (daily LLM summaries are retired), but
// PeriodContaining still computes day bounds + fire time fine.
func (s *MemoryScheduler) LazySeed(ctx context.Context, userID string, at time.Time) error {
	user, err := s.Users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("user not found")
	}
	p, err := timezone.PeriodContaining(
		at, user.Timezone, user.DayStartMinutes, user.ReflectionWeekday, domain.PeriodDay,
	)
	if err != nil {
		return err
	}
	if _, err := s.Jobs.Schedule(ctx, userID, p.Start, p.FireAtUTC); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("schedule memory job", "err", err, "user_id", userID)
		}
		return err
	}
	return nil
}
