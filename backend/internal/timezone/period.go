package timezone

import (
	"fmt"
	"time"

	"github.com/cosmosthrace/journai/backend/internal/domain"
)

// Period is the calendar bucket a summary covers, plus the absolute UTC
// instant the summary should fire. All fields are computed from the user's
// IANA zone + day_start_minutes — the client clock never participates.
//
// Start and End are inclusive: a daily Period has Start == End; a weekly
// Period spans Monday..Sunday inclusive. Both are "midnight in user's tz"
// instants, so they round-trip through `to_char(.., 'YYYY-MM-DD')`.
type Period struct {
	Type     domain.SummaryPeriod
	Start    time.Time // user-tz local midnight, inclusive
	End      time.Time // user-tz local midnight, inclusive
	FireAtUTC time.Time
}

// PeriodContaining returns the period of `pt` that the instant `at` belongs
// to in the user's day-start-shifted calendar.
//
// Day-start convention: a 1am instant under day_start_minutes=360 still
// belongs to the previous calendar day (and therefore to last week / last
// month if it crosses the boundary). LocalDate handles the shift; this fn
// just consumes that anchor date and rounds to period bounds.
//
// Fire time: end-of-period + 1 day, at (day_start + 30 min) in user-tz,
// converted to UTC. So daily(D)'s summary fires at the start of D+1's
// "user day", same for weekly/monthly/yearly. 30 minutes past the cutoff
// gives DST transitions and clock skew slack.
func PeriodContaining(at time.Time, iana string, dayStartMinutes int, pt domain.SummaryPeriod) (Period, error) {
	if !domain.IsValidPeriod(string(pt)) {
		return Period{}, fmt.Errorf("invalid period type %q", pt)
	}
	loc, err := time.LoadLocation(iana)
	if err != nil {
		return Period{}, fmt.Errorf("load timezone %q: %w", iana, err)
	}
	if dayStartMinutes < 0 || dayStartMinutes >= 1440 {
		return Period{}, fmt.Errorf("day_start_minutes out of range: %d", dayStartMinutes)
	}
	// Anchor: the calendar date `at` lives in for this user.
	anchor, err := LocalDate(at, iana, dayStartMinutes)
	if err != nil {
		return Period{}, err
	}

	var start, end time.Time
	switch pt {
	case domain.PeriodDay:
		start = anchor
		end = anchor
	case domain.PeriodWeek:
		// Monday-start (ISO 8601). time.Weekday(): Sunday=0, Monday=1, ..., Saturday=6.
		// daysFromMonday: Mon=0, Tue=1, ..., Sun=6.
		daysFromMonday := (int(anchor.Weekday()) + 6) % 7
		start = anchor.AddDate(0, 0, -daysFromMonday)
		end = start.AddDate(0, 0, 6)
	case domain.PeriodMonth:
		start = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
		// First of next month, then -1 day = last of current month.
		nextMonth := time.Date(anchor.Year(), anchor.Month()+1, 1, 0, 0, 0, 0, loc)
		end = nextMonth.AddDate(0, 0, -1)
	case domain.PeriodYear:
		start = time.Date(anchor.Year(), time.January, 1, 0, 0, 0, 0, loc)
		end = time.Date(anchor.Year(), time.December, 31, 0, 0, 0, 0, loc)
	}

	// fire_at: (end + 1 day) at (day_start + 30 min), in user-local time.
	// time.Date normalizes overflow (Day() == lastDay+1 → next month).
	fireMinutes := dayStartMinutes + 30
	fireLocal := time.Date(
		end.Year(), end.Month(), end.Day()+1,
		fireMinutes/60, fireMinutes%60, 0, 0, loc,
	)

	return Period{
		Type:      pt,
		Start:     start,
		End:       end,
		FireAtUTC: fireLocal.UTC(),
	}, nil
}

// PeriodFromLocalStart computes period bounds + fire time for a date that
// is already known to be a canonical user-local period_start (e.g. the
// `period_start` column from a summary_jobs row). Unlike
// PeriodContaining it does *not* re-run LocalDate — feeding a stored
// date back through LocalDate would re-apply the day_start_minutes
// shift and silently move the date backwards near the cutoff.
//
// Hour/minute fields of `localStart` are ignored; only the calendar
// date matters. The returned Period.Start equals localStart (re-anchored
// at user-tz midnight) for PeriodDay; for week/month/year the bounds
// are computed normally.
func PeriodFromLocalStart(
	localStart time.Time, iana string, dayStartMinutes int, pt domain.SummaryPeriod,
) (Period, error) {
	if !domain.IsValidPeriod(string(pt)) {
		return Period{}, fmt.Errorf("invalid period type %q", pt)
	}
	if dayStartMinutes < 0 || dayStartMinutes >= 1440 {
		return Period{}, fmt.Errorf("day_start_minutes out of range: %d", dayStartMinutes)
	}
	loc, err := time.LoadLocation(iana)
	if err != nil {
		return Period{}, fmt.Errorf("load timezone %q: %w", iana, err)
	}
	anchor := time.Date(
		localStart.Year(), localStart.Month(), localStart.Day(),
		0, 0, 0, 0, loc,
	)

	var start, end time.Time
	switch pt {
	case domain.PeriodDay:
		start = anchor
		end = anchor
	case domain.PeriodWeek:
		daysFromMonday := (int(anchor.Weekday()) + 6) % 7
		start = anchor.AddDate(0, 0, -daysFromMonday)
		end = start.AddDate(0, 0, 6)
	case domain.PeriodMonth:
		start = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
		nextMonth := time.Date(anchor.Year(), anchor.Month()+1, 1, 0, 0, 0, 0, loc)
		end = nextMonth.AddDate(0, 0, -1)
	case domain.PeriodYear:
		start = time.Date(anchor.Year(), time.January, 1, 0, 0, 0, 0, loc)
		end = time.Date(anchor.Year(), time.December, 31, 0, 0, 0, 0, loc)
	}

	fireMinutes := dayStartMinutes + 30
	fireLocal := time.Date(
		end.Year(), end.Month(), end.Day()+1,
		fireMinutes/60, fireMinutes%60, 0, 0, loc,
	)
	return Period{
		Type:      pt,
		Start:     start,
		End:       end,
		FireAtUTC: fireLocal.UTC(),
	}, nil
}

// NextPeriod returns the period immediately after `p` for the same type
// and timezone. Used by the worker after a summary fires to schedule the
// next slot.
//
// We compute by anchoring on (p.End + 1 day) as a known local date and
// running PeriodFromLocalStart. The "end + 1" is the canonical first
// day of the next period for every period type (daily/weekly/monthly/
// yearly), so it round-trips cleanly.
func NextPeriod(p Period, iana string, dayStartMinutes int) (Period, error) {
	nextDate := p.End.AddDate(0, 0, 1)
	return PeriodFromLocalStart(nextDate, iana, dayStartMinutes, p.Type)
}

// AllPeriods returns the four periods (day/week/month/year) containing
// `at`. Used at first-entry-write time to lazy-seed summary_jobs rows.
func AllPeriods(at time.Time, iana string, dayStartMinutes int) ([]Period, error) {
	out := make([]Period, 0, 4)
	for _, pt := range []domain.SummaryPeriod{
		domain.PeriodDay, domain.PeriodWeek, domain.PeriodMonth, domain.PeriodYear,
	} {
		p, err := PeriodContaining(at, iana, dayStartMinutes, pt)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
