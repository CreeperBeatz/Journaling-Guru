package domain

import "time"

// User mirrors the public-facing shape of a row in `users`. Soft-deleted
// users (deleted_at != NULL) are filtered out at the store layer; nothing
// downstream of the store should see them.
type User struct {
	ID                 string     `json:"id"`
	Email              string     `json:"email"`
	EmailVerified      bool       `json:"email_verified"`
	DisplayName        *string    `json:"display_name,omitempty"`
	Timezone           string     `json:"timezone"`
	TimezoneAuto       bool       `json:"timezone_auto"`
	ReminderTime       string     `json:"reminder_time"` // "HH:MM:SS"
	ReminderEnabled    bool       `json:"reminder_enabled"`
	DayStartMinutes    int        `json:"day_start_minutes"`
	ReflectionWeekday  int        `json:"reflection_weekday"` // 0=Sun..6=Sat
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	DeletedAt          *time.Time `json:"-"`
}
