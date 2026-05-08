-- +goose Up

-- Energy Audit pivot (see journaling-app-spec.md). Introduces three new
-- first-class primitives:
--
--   1. tags  — user-owned, valenced labels for recurring drainers/chargers.
--      ID is permanent; renaming updates label only so history stays
--      intact. normalized_label is the dedup key (lower(trim(label))).
--   2. daily_entry_tags — many-to-many link from a day to its drainer
--      and charger tags. Drives Zone 2 (top drainers/chargers) on the
--      summary page and the weekly reflection pattern view.
--   3. goals + goal_check_ins — the "commit to a change and measure it"
--      half of the loop. Each goal owns one yes/no daily check-in.
--
-- Plus a users.reflection_weekday column so the daily flow can swap to
-- the weekly reflection view on the chosen day.

CREATE TABLE tags (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label               citext      NOT NULL,
    normalized_label    citext      NOT NULL,
    valence             text        NOT NULL
        CHECK (valence IN ('positive','negative','neutral')),
    status              text        NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','merged','archived')),
    merged_into_tag_id  uuid        REFERENCES tags(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);

-- Idempotency anchor: chat extraction reuses an existing tag rather
-- than creating a duplicate by re-upserting on (user_id, normalized_label).
ALTER TABLE tags
    ADD CONSTRAINT tags_user_normalized_unique
    UNIQUE (user_id, normalized_label);

CREATE INDEX tags_user_status_valence_idx
    ON tags (user_id, status, valence)
    WHERE status = 'active';

CREATE TABLE daily_entry_tags (
    user_id     uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    local_date  date        NOT NULL,
    tag_id      uuid        NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    role        text        NOT NULL
        CHECK (role IN ('drainer','charger')),
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, local_date, tag_id, role)
);

-- Tag → days lookup for Zone 2: "give me every day this drainer appeared
-- in the last 30, and the mood on those days."
CREATE INDEX daily_entry_tags_tag_date_idx
    ON daily_entry_tags (user_id, tag_id, local_date DESC);

-- Day → tags lookup for rendering the day's pills back on /today and history.
CREATE INDEX daily_entry_tags_user_date_idx
    ON daily_entry_tags (user_id, local_date DESC, role);

CREATE TABLE goals (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             uuid        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title               text        NOT NULL,
    check_in_question   text        NOT NULL,
    start_date          date        NOT NULL,
    end_date            date        NOT NULL,
    status              text        NOT NULL DEFAULT 'active'
        CHECK (status IN ('active','completed','abandoned')),
    outcome             text
        CHECK (outcome IS NULL OR outcome IN ('kept','dropped','inconclusive')),
    conclusion_text     text        NOT NULL DEFAULT '',
    created_at          timestamptz NOT NULL DEFAULT now(),
    ended_at            timestamptz,
    CHECK (end_date >= start_date),
    CHECK ((status = 'active') = (outcome IS NULL AND ended_at IS NULL))
);

CREATE INDEX goals_user_status_end_idx
    ON goals (user_id, status, end_date);

CREATE TABLE goal_check_ins (
    goal_id     uuid        NOT NULL REFERENCES goals(id) ON DELETE CASCADE,
    local_date  date        NOT NULL,
    value       boolean     NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (goal_id, local_date)
);

-- For weekly progress and the kept-it count rendered on /summary Zone 3.
CREATE INDEX goal_check_ins_goal_date_idx
    ON goal_check_ins (goal_id, local_date DESC);

-- 0=Sunday..6=Saturday (matches Postgres EXTRACT(DOW)). Default 0 is
-- benign — onboarding writes the real value before the first reflection
-- can fire.
ALTER TABLE users
    ADD COLUMN reflection_weekday smallint NOT NULL DEFAULT 0
        CHECK (reflection_weekday BETWEEN 0 AND 6);

-- +goose Down
ALTER TABLE users DROP COLUMN reflection_weekday;
DROP TABLE IF EXISTS goal_check_ins;
DROP TABLE IF EXISTS goals;
DROP TABLE IF EXISTS daily_entry_tags;
DROP TABLE IF EXISTS tags;
