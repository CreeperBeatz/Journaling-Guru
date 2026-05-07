-- +goose Up

-- push_subscriptions stores the VAPID-encrypted Web Push endpoints the
-- worker fans a reminder out to. The browser produces these via
-- PushManager.subscribe(); the user_id ties them to one of our accounts,
-- and the global `endpoint` UUID acts as the dedup key.
--
-- Why endpoint is UNIQUE (no user scope): the push service URL is
-- already globally unique, and a re-subscribe from the same device
-- under a different account should win — overwriting the old binding,
-- not creating a parallel record. The user_id on the row reflects the
-- current owner.
--
-- p256dh / auth are base64url-encoded keys from the browser's
-- subscription.toJSON(); we store them as text and pass them to the
-- webpush-go encrypter on dispatch.
--
-- failed_count powers the back-off logic: 5xx / 4xx (other than 410)
-- bumps the counter; >= 5 failures deletes the row. 410 Gone deletes
-- immediately — the push service has authoritatively retired the
-- endpoint.
CREATE TABLE push_subscriptions (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint      text        NOT NULL UNIQUE,
    p256dh        text        NOT NULL,
    auth          text        NOT NULL,
    user_agent    text,
    last_used_at  timestamptz NOT NULL DEFAULT now(),
    failed_count  integer     NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);

-- reminder_jobs is the per-user nightly reminder queue. One row = "send
-- the reminder for user U at fire_at." Lazy pattern: on settings change
-- (reminder_time, reminder_enabled, timezone) we replan to a single
-- pending row; after each successful fire the worker schedules
-- tomorrow's row.
--
-- Same lifecycle vocabulary as summary_jobs (pending → claimed →
-- sent / skipped / failed) so the dispatcher tick in cmd/worker can
-- drain both queues with the same atomic FOR UPDATE SKIP LOCKED claim.
--
-- 'skipped' is used when fire_at arrives but the user has no active
-- subscriptions (or has reminder_enabled=false). We still mark the row
-- terminal so the dispatcher doesn't re-claim it; and we still schedule
-- tomorrow's row so subscribing later auto-resumes the cadence.
--
-- (user_id, fire_at) UNIQUE prevents accidental double-scheduling
-- during settings-driven replans across browser tabs.
CREATE TABLE reminder_jobs (
    id          uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    fire_at     timestamptz NOT NULL,
    fired_at    timestamptz,
    status      text        NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending','claimed','sent','skipped','failed')),
    attempts    integer     NOT NULL DEFAULT 0,
    last_error  text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE reminder_jobs
    ADD CONSTRAINT reminder_jobs_user_fire_unique
    UNIQUE (user_id, fire_at);

-- Dispatcher tick: claim due rows. Partial index keeps it tiny — only
-- pending rows are ever scanned, terminal rows stay out of the way.
CREATE INDEX reminder_jobs_due_idx
    ON reminder_jobs (fire_at)
    WHERE status = 'pending';

-- +goose Down
DROP TABLE IF EXISTS reminder_jobs;
DROP TABLE IF EXISTS push_subscriptions;
