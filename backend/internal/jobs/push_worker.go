package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/riverqueue/river"

	"github.com/cosmosthrace/journai/backend/internal/push"
	"github.com/cosmosthrace/journai/backend/internal/store"
)

// PushWorker handles ReminderArgs jobs. The dispatcher in cmd/worker
// hands one River job per due reminder_jobs row; this worker fans out
// to every push_subscriptions endpoint for that user, classifies each
// response, and schedules tomorrow's row.
type PushWorker struct {
	river.WorkerDefaults[ReminderArgs]

	Reminders     *store.ReminderJobStore
	Subscriptions *store.PushSubscriptionStore
	Users         *store.UserStore
	Sender        push.Sender
	Scheduler     *ReminderScheduler
	Logger        *slog.Logger

	// AppOrigin is the URL the SW opens on notificationclick. Without
	// it the click no-ops; we ship it in the payload so the SW doesn't
	// need to re-derive the host.
	AppOrigin string
}

// MaxFailedCount is the threshold past which a subscription is deleted
// without waiting for a 410 Gone. Conservative — push services
// typically issue Gone within a day of the endpoint going dead, so this
// is a backstop for misbehaving services.
const maxFailedCount = 5

// PushPayload is the JSON the SW receives. Fields are stable across
// versions so an old SW with a new server (or vice versa) still
// decodes them.
type PushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

// Work is River's entrypoint.
func (w *PushWorker) Work(ctx context.Context, rj *river.Job[ReminderArgs]) error {
	job, err := w.Reminders.GetByID(ctx, rj.Args.JobID)
	if err != nil {
		if errors.Is(err, store.ErrReminderJobNotFound) {
			// Cascaded delete (account removed) between dispatch and run.
			return nil
		}
		return err
	}
	// Terminal states: stale River retry can land here after our human
	// path moved the row.
	switch job.Status {
	case "sent", "skipped", "failed":
		return nil
	}

	user, err := w.Users.GetByID(ctx, job.UserID)
	if err != nil {
		return err
	}
	if user == nil {
		// User soft-deleted between scheduling and firing.
		return w.Reminders.MarkSkipped(ctx, job.ID, "user gone")
	}
	if !user.ReminderEnabled {
		// Settings flipped between scheduling and firing.
		_ = w.Reminders.MarkSkipped(ctx, job.ID, "reminders disabled")
		_ = w.Scheduler.ScheduleNext(ctx, user.ID, time.Now())
		return nil
	}

	subs, err := w.Subscriptions.ListByUser(ctx, user.ID)
	if err != nil {
		return w.releaseOrFail(ctx, job, rj, "list subscriptions: "+err.Error())
	}
	if len(subs) == 0 {
		// No devices to deliver to. Mark skipped + arm tomorrow so the
		// user resuming subscription on their phone next week reactivates
		// the cadence without an explicit Replan.
		_ = w.Reminders.MarkSkipped(ctx, job.ID, "no subscriptions")
		_ = w.Scheduler.ScheduleNext(ctx, user.ID, time.Now())
		return nil
	}

	if w.Sender == nil {
		// VAPID keys not configured. Don't keep retrying — the operator
		// fixes config, then re-enables reminders manually.
		_ = w.Reminders.MarkFailed(ctx, job.ID, "push sender not configured")
		return nil
	}

	payload, err := json.Marshal(PushPayload{
		Title: "Time to reflect",
		Body:  "Open JournAI to write today's entry.",
		URL:   strings.TrimRight(w.AppOrigin, "/") + "/today",
		Tag:   "reminder-" + job.ID,
	})
	if err != nil {
		return err
	}

	// Fan out and tally.
	var (
		delivered int
		gone      int
		retryable int
		lastErr   string
	)
	for _, sub := range subs {
		outcome, err := w.Sender.Send(ctx, push.Subscription{
			Endpoint: sub.Endpoint,
			P256dh:   sub.P256dh,
			Auth:     sub.Auth,
		}, payload)
		switch outcome {
		case push.OutcomeDelivered:
			delivered++
			w.Subscriptions.MarkSuccess(ctx, sub.ID)
		case push.OutcomeGone:
			gone++
			if err := w.Subscriptions.DeleteByID(ctx, sub.ID); err != nil {
				w.Logger.Warn("delete gone subscription", "err", err, "sub_id", sub.ID)
			}
		case push.OutcomeRetryable:
			retryable++
			if err != nil {
				lastErr = err.Error()
			}
			n, ferr := w.Subscriptions.IncrementFailure(ctx, sub.ID)
			if ferr != nil {
				w.Logger.Warn("increment failure", "err", ferr, "sub_id", sub.ID)
				continue
			}
			if n >= maxFailedCount {
				if derr := w.Subscriptions.DeleteByID(ctx, sub.ID); derr != nil {
					w.Logger.Warn("delete failing subscription", "err", derr, "sub_id", sub.ID)
				}
			}
		}
	}
	w.Logger.Info("reminder fan-out",
		"job_id", job.ID, "user_id", user.ID,
		"delivered", delivered, "gone", gone, "retryable", retryable,
	)

	// Success path: at least one delivery, OR every subscription is
	// permanently gone (no point retrying — re-subscribing creates
	// fresh rows anyway).
	if delivered > 0 || (retryable == 0 && gone == len(subs)) {
		if err := w.Reminders.MarkSent(ctx, job.ID); err != nil {
			return err
		}
		if err := w.Scheduler.ScheduleNext(ctx, user.ID, time.Now()); err != nil {
			w.Logger.Warn("schedule next reminder", "err", err, "user_id", user.ID)
		}
		return nil
	}

	// All endpoints transiently failed — bubble to River for retry.
	return w.releaseOrFail(ctx, job, rj, "all subscriptions transient: "+lastErr)
}

// releaseOrFail is the bridge between River's retry counter and our own
// reminder_jobs lifecycle. Same pattern as SummaryWorker.
func (w *PushWorker) releaseOrFail(
	ctx context.Context,
	job *store.ReminderJob,
	rj *river.Job[ReminderArgs],
	reason string,
) error {
	if rj.Attempt >= rj.MaxAttempts {
		_ = w.Reminders.MarkFailed(ctx, job.ID, reason)
		// Still arm tomorrow's slot — don't let one bad night break the
		// rest of the cadence.
		_ = w.Scheduler.ScheduleNext(ctx, job.UserID, time.Now())
		return nil
	}
	_ = w.Reminders.ReleaseForRetry(ctx, job.ID, reason)
	return errors.New(reason)
}
