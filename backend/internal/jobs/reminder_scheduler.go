package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/store"
)

// ReminderScheduler owns "what reminder_jobs row should exist for this
// user and when." Mirrors Scheduler (summaries) — the api calls Replan
// from the settings handler / on subscribe, the worker calls
// ScheduleNext after each successful fire.
//
// Why a separate type from the summary Scheduler: summaries are
// driven by writes ("user just journaled, seed all four periods");
// reminders are driven by config ("reminder_time changed, replan to
// next slot"). The lifecycles don't overlap, so combining them would
// only blur the entry points.
type ReminderScheduler struct {
	Jobs   *store.ReminderJobStore
	Users  *store.UserStore
	Logger *slog.Logger
}

// Replan rewrites a user's pending reminder_jobs row to match their
// current settings. Called when reminder_time / reminder_enabled /
// timezone changes, and when the user subscribes from a new device.
//
// Algorithm:
//  1. Drop pending rows (settings invalidated them).
//  2. If reminder_enabled, schedule the next absolute fire_at.
//
// Already-claimed/sent/skipped/failed rows are preserved as-is — they
// are either in flight (the worker still owns them) or part of the
// audit trail.
func (s *ReminderScheduler) Replan(ctx context.Context, userID string) error {
	user, err := s.Users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return errors.New("user not found")
	}
	if err := s.Jobs.DeletePendingForUser(ctx, userID); err != nil {
		return fmt.Errorf("clear pending: %w", err)
	}
	if !user.ReminderEnabled {
		return nil
	}
	fireAt, err := nextReminderFireAt(time.Now(), user.Timezone, user.ReminderTime)
	if err != nil {
		return err
	}
	if _, err := s.Jobs.Schedule(ctx, userID, fireAt); err != nil {
		return fmt.Errorf("schedule next: %w", err)
	}
	return nil
}

// ScheduleNext arms tomorrow's row after a successful (or skipped)
// fire. Skipped fires still re-arm so a user who subscribes a week
// after enabling reminders auto-resumes the cadence.
//
// If reminder_enabled has flipped off between scheduling and firing,
// don't queue another. The user can re-enable at any time and Replan
// will pick up.
func (s *ReminderScheduler) ScheduleNext(ctx context.Context, userID string, after time.Time) error {
	user, err := s.Users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if user == nil {
		// User soft-deleted — nothing to schedule.
		return nil
	}
	if !user.ReminderEnabled {
		return nil
	}
	fireAt, err := nextReminderFireAt(after, user.Timezone, user.ReminderTime)
	if err != nil {
		return err
	}
	if _, err := s.Jobs.Schedule(ctx, userID, fireAt); err != nil {
		return fmt.Errorf("schedule next: %w", err)
	}
	return nil
}

// nextReminderFireAt returns the next absolute UTC instant at which
// the user's reminder_time falls in their timezone, strictly after
// `after`. If today's reminder_time is already past, returns
// tomorrow's.
//
// We deliberately do NOT involve day_start_minutes — the reminder is
// keyed off clock time, not "user-day" semantics. A user with
// day_start=06:00 and reminder_time=20:00 still wants a 20:00-clock
// notification, not one shifted by their late-night cutoff.
func nextReminderFireAt(after time.Time, iana, reminderTime string) (time.Time, error) {
	loc, err := time.LoadLocation(iana)
	if err != nil {
		return time.Time{}, fmt.Errorf("load timezone %q: %w", iana, err)
	}
	hh, mm, err := parseHHMM(reminderTime)
	if err != nil {
		return time.Time{}, err
	}
	local := after.In(loc)
	candidate := time.Date(
		local.Year(), local.Month(), local.Day(),
		hh, mm, 0, 0, loc,
	)
	if !candidate.After(local) {
		candidate = candidate.AddDate(0, 0, 1)
	}
	return candidate.UTC(), nil
}

// parseHHMM accepts "HH:MM" or "HH:MM:SS" (matching the time format the
// users.reminder_time column emits). Seconds are ignored — reminders
// don't need second-resolution scheduling.
func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid reminder_time %q", s)
	}
	hh, err := strconv.Atoi(parts[0])
	if err != nil || hh < 0 || hh > 23 {
		return 0, 0, fmt.Errorf("invalid reminder_time hour in %q", s)
	}
	mm, err := strconv.Atoi(parts[1])
	if err != nil || mm < 0 || mm > 59 {
		return 0, 0, fmt.Errorf("invalid reminder_time minute in %q", s)
	}
	return hh, mm, nil
}
