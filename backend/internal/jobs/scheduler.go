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

// Scheduler owns "what summary_jobs row should exist and when". The
// worker consumes from the queue; the scheduler writes to it. Keeping
// these in one type lets the api binary call LazySeed on entry writes
// while the worker calls ScheduleNext after each fire — same logic
// either way.
type Scheduler struct {
	Jobs           *store.SummaryJobStore
	Users          *store.UserStore
	Logger         *slog.Logger
	InactivityDays int
}

// LazySeed inserts the four summary_jobs rows (day/week/month/year)
// covering the instant `at` for the given user, idempotent via ON
// CONFLICT DO NOTHING.
//
// Called from the entries.Upsert path on every successful write — the
// uniqueness constraint absorbs the duplicate writes so we don't need
// to first-check-then-insert. A wasted INSERT-RETURNING-no-row is
// cheaper than the round-trip to verify presence.
func (s *Scheduler) LazySeed(ctx context.Context, userID string, at time.Time) error {
	user, err := s.Users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("user not found")
	}
	periods, err := timezone.AllPeriods(at, user.Timezone, user.DayStartMinutes)
	if err != nil {
		return err
	}
	for _, p := range periods {
		if _, err := s.Jobs.Schedule(ctx, userID, string(p.Type), p.Start, p.FireAtUTC); err != nil {
			s.Logger.Warn("schedule lazy", "err", err, "user_id", userID, "period", p.Type)
			return err
		}
	}
	return nil
}

// ScheduleNext arms the next period for the same period_type, subject
// to a dormancy guard for day/week/month. Yearly always re-arms — the
// year-in-review fires on Jan 1 regardless of how active the user has
// been.
//
// "Dormant" = no journal entries within the configured InactivityDays
// window. When such users return and write, LazySeed re-engages the
// cadence — so dormancy doesn't permanently disable summaries, it just
// pauses them.
func (s *Scheduler) ScheduleNext(ctx context.Context, job *domain.SummaryJob) error {
	user, err := s.Users.GetByID(ctx, job.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		// User soft-deleted — nothing to schedule.
		return nil
	}

	if domain.SummaryPeriod(job.PeriodType) != domain.PeriodYear {
		dormant, err := s.isDormant(ctx, user.ID)
		if err != nil {
			return err
		}
		if dormant {
			s.Logger.Info("skip schedule next: dormant user",
				"user_id", user.ID, "period", job.PeriodType,
				"inactivity_days", s.InactivityDays,
			)
			return nil
		}
	}

	periodStart, err := time.Parse("2006-01-02", job.PeriodStart)
	if err != nil {
		return err
	}
	// PeriodFromLocalStart preserves the stored date as canonical; see
	// timezone/period.go for why PeriodContaining would shift it.
	curr, err := timezone.PeriodFromLocalStart(
		periodStart, user.Timezone, user.DayStartMinutes,
		domain.SummaryPeriod(job.PeriodType),
	)
	if err != nil {
		return err
	}
	next, err := timezone.NextPeriod(curr, user.Timezone, user.DayStartMinutes)
	if err != nil {
		return err
	}
	if _, err := s.Jobs.Schedule(ctx, user.ID, string(next.Type), next.Start, next.FireAtUTC); err != nil {
		return err
	}
	return nil
}

// isDormant returns true when the user has no entries within the
// configured window. Zero entries (brand-new user with one period seed
// but no writes) also counts as dormant — they shouldn't be churning
// daily LLM no-ops.
func (s *Scheduler) isDormant(ctx context.Context, userID string) (bool, error) {
	last, err := s.Jobs.LastEntryDate(ctx, userID)
	if err != nil {
		return false, err
	}
	if last.IsZero() || last.Year() == 1 {
		return true, nil
	}
	cutoff := time.Now().AddDate(0, 0, -s.InactivityDays)
	return last.Before(cutoff), nil
}
