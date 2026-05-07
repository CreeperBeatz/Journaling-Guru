package jobs

import "github.com/riverqueue/river"

// ReminderArgs is the River payload for a single reminder fan-out. As
// with SummaryArgs, only the queue-row id is transported — the worker
// reads everything else from Postgres so retry semantics stay simple.
type ReminderArgs struct {
	JobID string `json:"job_id"`
}

func (ReminderArgs) Kind() string { return "reminder" }

// MaxAttempts is conservative: pushing to web push services is fast and
// ephemeral, so we don't want a backlog of retries piling up if the
// service is genuinely down. River's exponential backoff plus our own
// dispatcher tick will retry a transient-failed row anyway.
func (ReminderArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       river.QueueDefault,
		MaxAttempts: 3,
	}
}
