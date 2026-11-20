package model

import "time"

// Repeat units, monthly modes and end kinds of a Recurrence.
const (
	RepeatOff     = "off"
	RepeatDaily   = "daily"
	RepeatWeekly  = "weekly"
	RepeatMonthly = "monthly"
	RepeatYearly  = "yearly"

	ByWeekday  = "weekday"  // monthly on the same nth weekday as the first job, e.g. the 3rd Friday
	ByMonthday = "monthday" // monthly on the same day of the month as the first job

	EndsNever  = "never"
	EndsAfter  = "after"
	EndsOnDate = "on_date"
)

// Recurrence is how often a template's jobs repeat.
type Recurrence struct {
	Unit      string
	Every     int
	By        string // monthly only; empty means ByMonthday
	EndsKind  string
	EndsAfter int
	EndsOn    *time.Time // a calendar date held as midnight UTC
}

// Repeats reports whether the template makes a series rather than a single job.
func (r Recurrence) Repeats() bool { return r.Unit != RepeatOff && r.Unit != "" }

// Days returns the dates of the series from first through last, both inclusive, in order.
// A month that lacks the day (the 31st, a 5th Friday) or a missing 29 February gives no job that
// period, and the end rule "after N" counts only jobs that exist. Dates are calendar dates held as
// midnight UTC, the same convention as ParseDate.
func (r Recurrence) Days(first, last time.Time) []time.Time {
	if r.EndsKind == EndsOnDate && r.EndsOn != nil && r.EndsOn.Before(last) {
		last = *r.EndsOn
	}
	if first.After(last) {
		return nil
	}
	if !r.Repeats() {
		return []time.Time{first}
	}
	every := max(r.Every, 1)
	var out []time.Time
	for k := 0; ; k++ {
		d, ok := r.nth(first, k*every)
		if d.After(last) {
			return out
		}
		if !ok {
			continue
		}
		out = append(out, d)
		if r.EndsKind == EndsAfter && len(out) >= r.EndsAfter {
			return out
		}
	}
}

// nth is the date `steps` periods after first. ok is false when that period has no such date; d
// still moves forward then, so the caller's loop ends.
func (r Recurrence) nth(first time.Time, steps int) (d time.Time, ok bool) {
	y, m, day := first.Date()
	switch r.Unit {
	case RepeatDaily:
		return first.AddDate(0, 0, steps), true
	case RepeatWeekly:
		return first.AddDate(0, 0, 7*steps), true
	case RepeatYearly:
		d = civil(y+steps, m, day)
		return d, d.Month() == m
	}
	month := civil(y, m+time.Month(steps), 1)
	if r.By == ByWeekday {
		// The same nth weekday: the first matching weekday of the month, then n-1 weeks later.
		offset := (int(first.Weekday()) - int(month.Weekday()) + 7) % 7
		d = month.AddDate(0, 0, offset+7*((day-1)/7))
		return d, d.Month() == month.Month()
	}
	d = civil(month.Year(), month.Month(), day)
	return d, d.Month() == month.Month()
}
