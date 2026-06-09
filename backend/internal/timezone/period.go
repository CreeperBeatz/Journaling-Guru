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
	Type      domain.SummaryPeriod
	Start     time.Time // user-tz local midnight, inclusive
	End       time.Time // user-tz local midnight, inclusive
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
// Weekly anchoring: the week ENDS on the user's reflection_weekday and
// starts 6 days before it. This way the worker's stored period_start /
// period_end match the wizard's "today - 6 days .. today" window the
// FE renders on reflection day, and fire_at can land at the START of
// reflection day so the letter is ready when the user opens the wizard.
// reflectionWeekday is 0=Sun..6=Sat (matches Go's time.Weekday).
//
// Fire time:
//   - weekly: period_end (= reflection_weekday) at day_start_minutes, in
//     user-tz. The synthesis fires at the start of reflection day so
//     it's ready when the user begins reflecting.
//   - monthly: the first reflection_weekday on-or-after period_end (the
//     user's "monthly day"), at day_start_minutes + 15 — quarter past the
//     weekly job that fires the same morning, so the final weekly letter
//     normally exists when the monthly synthesis composes over it.
//   - day/year (dead under Energy Audit): end+1 at day_start+30,
//     preserved for back-compat in case those paths are ever revived.
func PeriodContaining(at time.Time, iana string, dayStartMinutes, reflectionWeekday int, pt domain.SummaryPeriod) (Period, error) {
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
	if reflectionWeekday < 0 || reflectionWeekday > 6 {
		return Period{}, fmt.Errorf("reflection_weekday out of range: %d", reflectionWeekday)
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
		// Week ends on the user's reflection_weekday. Find the most
		// recent such weekday at-or-equal to `anchor`. daysSinceRefl:
		// 0 if today IS reflection_weekday, 1..6 otherwise.
		daysSinceRefl := (int(anchor.Weekday()) - reflectionWeekday + 7) % 7
		end = anchor.AddDate(0, 0, -daysSinceRefl)
		start = end.AddDate(0, 0, -6)
	case domain.PeriodMonth:
		start = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
		nextMonth := time.Date(anchor.Year(), anchor.Month()+1, 1, 0, 0, 0, 0, loc)
		end = nextMonth.AddDate(0, 0, -1)
	case domain.PeriodYear:
		start = time.Date(anchor.Year(), time.January, 1, 0, 0, 0, 0, loc)
		end = time.Date(anchor.Year(), time.December, 31, 0, 0, 0, 0, loc)
	}

	return Period{
		Type:      pt,
		Start:     start,
		End:       end,
		FireAtUTC: computeFireAt(pt, end, dayStartMinutes, reflectionWeekday, loc),
	}, nil
}

// computeFireAt resolves the absolute UTC moment a job for `pt` ending on
// `end` (user-tz local midnight, inclusive) should fire. Weekly fires at
// the START of period_end (reflection_weekday at day_start) so the
// synthesis is ready as the user begins their reflection day. Monthly
// fires on the first reflection_weekday on-or-after period_end at
// day_start + 15 — same morning as that week's weekly job, just after it,
// so the monthly composes over a complete set of weekly letters. Other
// period types use end+1 at day_start+30 (legacy convention).
func computeFireAt(pt domain.SummaryPeriod, end time.Time, dayStartMinutes, reflectionWeekday int, loc *time.Location) time.Time {
	switch pt {
	case domain.PeriodWeek:
		fireLocal := time.Date(
			end.Year(), end.Month(), end.Day(),
			dayStartMinutes/60, dayStartMinutes%60, 0, 0, loc,
		)
		return fireLocal.UTC()
	case domain.PeriodMonth:
		// First reflection_weekday on-or-after the last day of the month
		// (0 days ahead when month-end IS the reflection day).
		daysUntilRefl := (reflectionWeekday - int(end.Weekday()) + 7) % 7
		fireMinutes := dayStartMinutes + 15
		fireLocal := time.Date(
			end.Year(), end.Month(), end.Day()+daysUntilRefl,
			fireMinutes/60, fireMinutes%60, 0, 0, loc,
		)
		return fireLocal.UTC()
	}
	fireMinutes := dayStartMinutes + 30
	fireLocal := time.Date(
		end.Year(), end.Month(), end.Day()+1,
		fireMinutes/60, fireMinutes%60, 0, 0, loc,
	)
	return fireLocal.UTC()
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
	localStart time.Time, iana string, dayStartMinutes, reflectionWeekday int, pt domain.SummaryPeriod,
) (Period, error) {
	if !domain.IsValidPeriod(string(pt)) {
		return Period{}, fmt.Errorf("invalid period type %q", pt)
	}
	if dayStartMinutes < 0 || dayStartMinutes >= 1440 {
		return Period{}, fmt.Errorf("day_start_minutes out of range: %d", dayStartMinutes)
	}
	if reflectionWeekday < 0 || reflectionWeekday > 6 {
		return Period{}, fmt.Errorf("reflection_weekday out of range: %d", reflectionWeekday)
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
		// localStart IS the canonical period_start (= reflection_weekday - 6).
		// End = start + 6 days = reflection_weekday.
		start = anchor
		end = start.AddDate(0, 0, 6)
	case domain.PeriodMonth:
		start = time.Date(anchor.Year(), anchor.Month(), 1, 0, 0, 0, 0, loc)
		nextMonth := time.Date(anchor.Year(), anchor.Month()+1, 1, 0, 0, 0, 0, loc)
		end = nextMonth.AddDate(0, 0, -1)
	case domain.PeriodYear:
		start = time.Date(anchor.Year(), time.January, 1, 0, 0, 0, 0, loc)
		end = time.Date(anchor.Year(), time.December, 31, 0, 0, 0, 0, loc)
	}

	return Period{
		Type:      pt,
		Start:     start,
		End:       end,
		FireAtUTC: computeFireAt(pt, end, dayStartMinutes, reflectionWeekday, loc),
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
func NextPeriod(p Period, iana string, dayStartMinutes, reflectionWeekday int) (Period, error) {
	nextDate := p.End.AddDate(0, 0, 1)
	return PeriodFromLocalStart(nextDate, iana, dayStartMinutes, reflectionWeekday, p.Type)
}

// AllPeriods returns the surviving periods containing `at`. Under the
// Energy Audit pivot the weekly summary fires, plus the monthly synthesis
// (re-admitted by the monthly reflection loop — it composes over weekly
// artifacts, not raw entries); the daily / yearly LLM summaries stay
// retired.
func AllPeriods(at time.Time, iana string, dayStartMinutes, reflectionWeekday int) ([]Period, error) {
	out := make([]Period, 0, 2)
	for _, pt := range []domain.SummaryPeriod{
		domain.PeriodWeek,
		domain.PeriodMonth,
	} {
		p, err := PeriodContaining(at, iana, dayStartMinutes, reflectionWeekday, pt)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// MonthlyWeekFor reports whether the canonical reflection week ending on
// `weekEnd` hosts a monthly reflection, and for which calendar month. A
// week is the "monthly week" for month M when M's last day falls inside
// [weekEnd-13, weekEnd]: the week containing month-end's first
// on-or-after reflection day, plus ONE carry-over grace week for users who
// miss it. Callers layer the monthly_reflections.completed_at check on
// top — a completed month stops claiming weeks regardless of the window.
//
// weekEnd must be a canonical user-local midnight (a stored date — never
// re-normalize it through LocalDate).
func MonthlyWeekFor(weekEnd time.Time, loc *time.Location) (monthStart, monthEnd time.Time, ok bool) {
	// The candidate month is the most recent month whose end is <= weekEnd:
	// weekEnd's own month if weekEnd is its last day, else the month before.
	firstOfWeekEndMonth := time.Date(weekEnd.Year(), weekEnd.Month(), 1, 0, 0, 0, 0, loc)
	candEnd := firstOfWeekEndMonth.AddDate(0, 1, 0).AddDate(0, 0, -1) // end of weekEnd's month
	if candEnd.After(weekEnd) {
		candEnd = firstOfWeekEndMonth.AddDate(0, 0, -1) // end of previous month
	}
	if candEnd.Before(weekEnd.AddDate(0, 0, -13)) {
		return time.Time{}, time.Time{}, false
	}
	monthStart = time.Date(candEnd.Year(), candEnd.Month(), 1, 0, 0, 0, 0, loc)
	return monthStart, candEnd, true
}
