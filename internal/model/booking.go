package model

import (
	"fmt"
	"time"
)

// Booking is a booked visit and the estimate it created.
type Booking struct {
	ID          int64
	CustomerID  int64
	Service     Service
	Location    Location
	Estimate    BillingDocument // ID, Number, Status and TotalCents are set by reads
	StartsAt    time.Time
	EndsAt      time.Time
	CancelledAt *time.Time
}

// Interval is a busy stretch of time, half-open: [Start, End).
type Interval struct {
	Start, End time.Time
}

// Hours are the opening hours, the same every day, in the business timezone.
// Open and Close are minutes after local midnight.
type Hours struct {
	Loc         *time.Location
	Open        int
	Close       int
	Step        int
	HorizonDays int
}

// Dates are calendar dates held as midnight UTC, the same convention as document dates.
func civil(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// ParseDate reads a YYYY-MM-DD calendar date.
func ParseDate(s string) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: date must be a real YYYY-MM-DD date", ErrValidation)
	}
	return d, nil
}

// BookableRange is the first and last bookable date: tomorrow through today + HorizonDays,
// where "today" is the calendar day in the business timezone.
func (h Hours) BookableRange(now time.Time) (first, last time.Time) {
	y, m, d := now.In(h.Loc).Date()
	return civil(y, m, d+1), civil(y, m, d+h.HorizonDays)
}

func (h Hours) checkBookable(now, day time.Time) error {
	first, last := h.BookableRange(now)
	if day.Before(first) || day.After(last) {
		return fmt.Errorf("%w: bookable dates are %s to %s", ErrValidation,
			first.Format(time.DateOnly), last.Format(time.DateOnly))
	}
	return nil
}

// Days returns up to seven bookable dates from `from`, and the `from` of the previous and
// next pages (nil at the edges of the bookable range).
func (h Hours) Days(now, from time.Time) (days []time.Time, prev, next *time.Time, err error) {
	if err := h.checkBookable(now, from); err != nil {
		return nil, nil, nil, err
	}
	first, last := h.BookableRange(now)
	for d := from; len(days) < 7 && !d.After(last); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	if from.After(first) {
		p := from.AddDate(0, 0, -7)
		if p.Before(first) {
			p = first
		}
		prev = &p
	}
	if n := from.AddDate(0, 0, 7); !n.After(last) {
		next = &n
	}
	return days, prev, next, nil
}

// at is the absolute time of `minutes` after midnight on `day`, in the business timezone.
func (h Hours) at(day time.Time, minutes int) time.Time {
	y, m, d := day.Date()
	return time.Date(y, m, d, minutes/60, minutes%60, 0, 0, h.Loc)
}

// DayBounds is local midnight to the next local midnight, which is not always 24 hours.
func (h Hours) DayBounds(day time.Time) (start, end time.Time) {
	return h.at(day, 0), h.at(day.AddDate(0, 0, 1), 0)
}

// ParseSlot validates a booking request's date and HH:MM time for a visit of durationMin
// minutes and returns its absolute start time.
func (h Hours) ParseSlot(now time.Time, dateStr, timeStr string, durationMin int) (time.Time, error) {
	day, err := ParseDate(dateStr)
	if err != nil {
		return time.Time{}, err
	}
	if err := h.checkBookable(now, day); err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse("15:04", timeStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: time must be HH:MM", ErrValidation)
	}
	minutes := t.Hour()*60 + t.Minute()
	if minutes < h.Open || minutes+durationMin > h.Close || (minutes-h.Open)%h.Step != 0 {
		return time.Time{}, fmt.Errorf("%w: %s is not a start time for a %d-minute visit", ErrValidation, timeStr, durationMin)
	}
	return h.at(day, minutes), nil
}

// FreeSlots lists the HH:MM start times on `day` where a visit of durationMin minutes fits
// before closing and overlaps none of the busy intervals. Never nil.
func (h Hours) FreeSlots(day time.Time, durationMin int, busy []Interval) []string {
	slots := []string{}
	length := time.Duration(durationMin) * time.Minute
	for m := h.Open; m+durationMin <= h.Close; m += h.Step {
		start := h.at(day, m)
		end := start.Add(length)
		free := true
		for _, b := range busy {
			if b.Start.Before(end) && start.Before(b.End) {
				free = false
				break
			}
		}
		if free {
			slots = append(slots, start.Format("15:04"))
		}
	}
	return slots
}
