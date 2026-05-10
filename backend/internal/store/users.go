package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// UserStore reads and writes the `users` table. Soft-deleted rows are
// filtered out everywhere — callers never see deleted_at.
type UserStore struct {
	DB *pgxpool.Pool
}

func NewUserStore(db *pgxpool.Pool) *UserStore { return &UserStore{DB: db} }

const userColumns = `id, email, email_verified, display_name, timezone, timezone_auto,
    to_char(reminder_time, 'HH24:MI:SS') AS reminder_time,
    reminder_enabled, day_start_minutes, reflection_weekday,
    created_at, updated_at, deleted_at`

func scanUser(row pgx.Row) (*domain.User, error) {
	var u domain.User
	if err := row.Scan(
		&u.ID, &u.Email, &u.EmailVerified, &u.DisplayName, &u.Timezone, &u.TimezoneAuto,
		&u.ReminderTime, &u.ReminderEnabled, &u.DayStartMinutes, &u.ReflectionWeekday,
		&u.CreatedAt, &u.UpdatedAt, &u.DeletedAt,
	); err != nil {
		return nil, err
	}
	return &u, nil
}

// UpsertByEmail returns the user for `email`, creating an unverified row if
// none exists. The returned user is always non-nil on success and never
// soft-deleted (we re-activate by clearing deleted_at on conflict).
func (s *UserStore) UpsertByEmail(ctx context.Context, email string) (*domain.User, error) {
	const q = `
		INSERT INTO users (email)
		VALUES ($1)
		ON CONFLICT (email) DO UPDATE
		    SET deleted_at = NULL,
		        updated_at = now()
		RETURNING ` + userColumns
	return scanUser(s.DB.QueryRow(ctx, q, email))
}

// GetByID returns the user, or nil if absent or soft-deleted.
func (s *UserStore) GetByID(ctx context.Context, id string) (*domain.User, error) {
	const q = `SELECT ` + userColumns + ` FROM users WHERE id = $1 AND deleted_at IS NULL`
	u, err := scanUser(s.DB.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

// MarkEmailVerified flips email_verified=true. Idempotent — successive calls
// are no-ops once the flag is set.
func (s *UserStore) MarkEmailVerified(ctx context.Context, id string) error {
	const q = `UPDATE users SET email_verified = true, updated_at = now()
	            WHERE id = $1 AND deleted_at IS NULL`
	_, err := s.DB.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	return nil
}

// SettingsPatch carries the subset of users.* fields the user can edit
// from the Settings page. Nil fields are skipped — partial updates are
// the norm, e.g. timezone-only when the browser detects the user moved.
type SettingsPatch struct {
	DisplayName       *string
	Timezone          *string
	TimezoneAuto      *bool
	ReminderTime      *string // "HH:MM" or "HH:MM:SS"
	ReminderEnabled   *bool
	DayStartMinutes   *int
	ReflectionWeekday *int // 0=Sun..6=Sat
}

// UpdateSettings applies the patch and returns the resulting row. Callers
// must pre-validate the timezone (we don't load tzdata in the SQL layer).
func (s *UserStore) UpdateSettings(ctx context.Context, id string, p SettingsPatch) (*domain.User, error) {
	const q = `
		UPDATE users
		   SET display_name        = COALESCE($2, display_name),
		       timezone            = COALESCE($3, timezone),
		       timezone_auto       = COALESCE($4, timezone_auto),
		       reminder_time       = COALESCE($5::time, reminder_time),
		       reminder_enabled    = COALESCE($6, reminder_enabled),
		       day_start_minutes   = COALESCE($7, day_start_minutes),
		       reflection_weekday  = COALESCE($8, reflection_weekday),
		       updated_at          = now()
		 WHERE id = $1 AND deleted_at IS NULL
		 RETURNING ` + userColumns
	row := s.DB.QueryRow(ctx, q, id, p.DisplayName, p.Timezone, p.TimezoneAuto, p.ReminderTime,
		p.ReminderEnabled, p.DayStartMinutes, p.ReflectionWeekday)
	u, err := scanUser(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

// MaybeAutoSyncTimezone updates users.timezone to browserTZ only when the
// user is in auto mode and the value actually differs. Returns the post-
// update user (or the original row if nothing changed) and `changed=true`
// when the UPDATE took effect — callers use that to decide whether to
// kick the reminder Replanner.
func (s *UserStore) MaybeAutoSyncTimezone(ctx context.Context, id, browserTZ string) (*domain.User, bool, error) {
	const q = `
		UPDATE users
		   SET timezone = $2,
		       updated_at = now()
		 WHERE id = $1
		   AND deleted_at IS NULL
		   AND timezone_auto = true
		   AND timezone <> $2
		 RETURNING ` + userColumns
	u, err := scanUser(s.DB.QueryRow(ctx, q, id, browserTZ))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return u, true, nil
}

// SoftDelete sets deleted_at and cascades to dependent rows we need cleared
// for security (sessions, magic_link_tokens). Other domain rows (entries,
// summaries) stay until a hard-delete job runs — out of scope for Phase 2.
func (s *UserStore) SoftDelete(ctx context.Context, id string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
		return fmt.Errorf("delete sessions: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM magic_link_tokens WHERE user_id = $1`, id); err != nil {
		return fmt.Errorf("delete magic-link tokens: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE users SET deleted_at = now(), updated_at = now()
		   WHERE id = $1 AND deleted_at IS NULL`, id); err != nil {
		return fmt.Errorf("soft delete user: %w", err)
	}
	return tx.Commit(ctx)
}
