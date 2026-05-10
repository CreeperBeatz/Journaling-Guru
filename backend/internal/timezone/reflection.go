package timezone

import (
	"fmt"
	"time"
)

// NextReflectionWeekday returns the date of the Nth reflection_weekday
// strictly after `from`. `weekday` is 0=Sunday..6=Saturday (matching
// Postgres EXTRACT(DOW) and users.reflection_weekday).
//
// Used to align goal end_dates to a reflection day: a 1-week goal
// created on a Wednesday with reflection_weekday=Sunday lands on the
// upcoming Sunday (4 days later), not 7. A 2-week goal lands on the
// Sunday after that. This guarantees every goal terminates inside a
// weekly reflection.
//
// `n` must be ≥ 1. The returned date is at midnight in `from`'s zone.
func NextReflectionWeekday(from time.Time, weekday, n int) (time.Time, error) {
	if weekday < 0 || weekday > 6 {
		return time.Time{}, fmt.Errorf("weekday out of range: %d", weekday)
	}
	if n < 1 {
		return time.Time{}, fmt.Errorf("n must be >= 1, got %d", n)
	}
	// Days to the next occurrence of `weekday` strictly after `from`.
	// (from.Weekday() == weekday) → 7 (skip today, go to next week).
	current := int(from.Weekday())
	diff := (weekday - current + 7) % 7
	if diff == 0 {
		diff = 7
	}
	days := diff + (n-1)*7
	return time.Date(
		from.Year(), from.Month(), from.Day()+days,
		0, 0, 0, 0, from.Location(),
	), nil
}
