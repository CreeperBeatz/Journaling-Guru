// Package timezone converts between an instant + IANA zone and the calendar
// date that instant lives in for the user. Every write that maps to "today"
// runs through this — never trust the client clock.
package timezone

import (
	"fmt"
	"time"
)

// LocalDate returns the calendar date that `at` falls on in `iana`, with
// the day-rollover boundary shifted by `dayStartMinutes` from midnight.
//
// `dayStartMinutes` ∈ [0, 1440). 0 = midnight (calendar default).
// 360 = 06:00; under that setting, anything from 00:00 to 05:59 still
// belongs to the previous calendar date. This matches how late-night
// journalers think about "their day" — a 1am reflection is for yesterday.
//
// Returns an error for an unknown / empty zone or out-of-range offset;
// callers must surface this rather than silently defaulting, otherwise a
// misconfigured user would double-write entries for the same day.
func LocalDate(at time.Time, iana string, dayStartMinutes int) (time.Time, error) {
	if iana == "" {
		return time.Time{}, fmt.Errorf("timezone is empty")
	}
	if dayStartMinutes < 0 || dayStartMinutes >= 1440 {
		return time.Time{}, fmt.Errorf("day_start_minutes out of range: %d", dayStartMinutes)
	}
	loc, err := time.LoadLocation(iana)
	if err != nil {
		return time.Time{}, fmt.Errorf("load timezone %q: %w", iana, err)
	}
	t := at.In(loc).Add(-time.Duration(dayStartMinutes) * time.Minute)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc), nil
}

// IsValidIANA reports whether `iana` resolves via the runtime's tzdata. We
// treat empty as invalid — callers should default to "UTC" if they need a
// fallback, not to "".
func IsValidIANA(iana string) bool {
	if iana == "" {
		return false
	}
	_, err := time.LoadLocation(iana)
	return err == nil
}

// FormatDate returns YYYY-MM-DD; the canonical wire format for local_date
// across the API. Date-only — no time component, no zone suffix.
func FormatDate(d time.Time) string {
	return d.Format("2006-01-02")
}
