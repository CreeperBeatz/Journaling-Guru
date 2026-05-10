-- +goose Up
-- Track whether a user has completed the first-run walkthrough at /onboarding.
-- NULL means "show the walkthrough on next /api/me load"; a timestamp means
-- "skip and let them go straight to /today." The Settings → "Replay
-- walkthrough" link routes to /onboarding?replay=1 without resetting this
-- column — replay is cosmetic and doesn't re-trigger the gate.
--
-- Existing users (pre-onboarding) are backfilled to now() so the rollout
-- doesn't force-walk anyone who has already been using the app. To test
-- the flow on yourself: UPDATE users SET onboarded_at = NULL WHERE id = ?.
ALTER TABLE users
    ADD COLUMN onboarded_at timestamptz;

UPDATE users
   SET onboarded_at = now()
 WHERE onboarded_at IS NULL;

-- +goose Down
ALTER TABLE users DROP COLUMN onboarded_at;
