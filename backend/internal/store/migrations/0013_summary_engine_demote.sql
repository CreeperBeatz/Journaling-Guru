-- +goose Up

-- Energy Audit pivot retires three of the four summary periods. Per
-- spec: "no daily AI summary, no AI recap" — the user wrote the data,
-- they don't need it paraphrased back. Monthly + yearly are not
-- referenced anywhere in the new product surface. Only the weekly
-- summary survives, demoted to a single-sentence headline insight that
-- feeds Zone 1 of the always-available summary page.
--
-- This migration cancels in-flight scheduling for the retired periods
-- and tightens the CHECK constraint so future enqueues fail fast. It
-- does not delete historical `summaries` rows (they're cheap to keep
-- and may be useful as training data later) — only the `summary_jobs`
-- queue is constrained.

UPDATE summary_jobs
   SET status = 'cancelled',
       updated_at = now()
 WHERE period_type IN ('day','month','year')
   AND status IN ('pending','claimed','failed');

-- The summary_jobs status column allows 'cancelled' already (see
-- 0004_summaries.sql). The CHECK on period_type is what we tighten.
ALTER TABLE summary_jobs
    DROP CONSTRAINT IF EXISTS summary_jobs_period_type_check;
ALTER TABLE summary_jobs
    ADD CONSTRAINT summary_jobs_period_type_check
    CHECK (period_type = 'week');

-- summaries.period_type stays permissive — historical rows remain
-- readable, even if no new daily/monthly/yearly rows will ever be
-- written.

-- +goose Down
ALTER TABLE summary_jobs
    DROP CONSTRAINT IF EXISTS summary_jobs_period_type_check;
ALTER TABLE summary_jobs
    ADD CONSTRAINT summary_jobs_period_type_check
    CHECK (period_type IN ('day','week','month','year'));
