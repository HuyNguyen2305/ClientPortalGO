package model_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/model"
)

func chicago(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Chicago")
	require.NoError(t, err)
	return loc
}

// hours: 07:00 to 17:00, 15-minute grid, 90 days ahead, Chicago time.
func hours(t *testing.T) model.Hours {
	return model.Hours{Loc: chicago(t), Open: 7 * 60, Close: 17 * 60, Step: 15, HorizonDays: 90}
}

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestBookableRange(t *testing.T) {
	h := hours(t)
	// 23:30 on Nov 11 in Chicago is already Nov 12 in UTC; "today" is still the 11th.
	now := time.Date(2026, 11, 11, 23, 30, 0, 0, h.Loc)
	first, last := h.BookableRange(now)
	assert.Equal(t, date(2026, 11, 12), first)
	assert.Equal(t, date(2027, 2, 9), last)

	// 00:30 on Nov 12 in Chicago: tomorrow is the 13th.
	first, _ = h.BookableRange(time.Date(2026, 11, 12, 0, 30, 0, 0, h.Loc))
	assert.Equal(t, date(2026, 11, 13), first)
}

func TestParseDate(t *testing.T) {
	got, err := model.ParseDate("2026-11-12")
	require.NoError(t, err)
	assert.Equal(t, date(2026, 11, 12), got)
	for _, bad := range []string{"", "2026-13-01", "12/11/2026", "2026-11-31"} {
		_, err := model.ParseDate(bad)
		assert.True(t, errors.Is(err, model.ErrValidation), "%q: %v", bad, err)
	}
}

func TestDays(t *testing.T) {
	h := hours(t)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, h.Loc) // bookable: Nov 12 .. Feb 9

	t.Run("first page", func(t *testing.T) {
		days, prev, next, err := h.Days(now, date(2026, 11, 12))
		require.NoError(t, err)
		require.Len(t, days, 7)
		assert.Equal(t, date(2026, 11, 12), days[0])
		assert.Equal(t, date(2026, 11, 18), days[6])
		assert.Nil(t, prev)
		require.NotNil(t, next)
		assert.Equal(t, date(2026, 11, 19), *next)
	})

	t.Run("previous is clipped to tomorrow", func(t *testing.T) {
		_, prev, _, err := h.Days(now, date(2026, 11, 15))
		require.NoError(t, err)
		require.NotNil(t, prev)
		assert.Equal(t, date(2026, 11, 12), *prev)
	})

	t.Run("last page is short and has no next", func(t *testing.T) {
		days, prev, next, err := h.Days(now, date(2027, 2, 6))
		require.NoError(t, err)
		assert.Len(t, days, 4) // Feb 6, 7, 8, 9
		assert.Nil(t, next)
		require.NotNil(t, prev)
		assert.Equal(t, date(2027, 1, 30), *prev)
	})

	t.Run("outside the range", func(t *testing.T) {
		for _, from := range []time.Time{date(2026, 11, 11), date(2027, 2, 10)} {
			_, _, _, err := h.Days(now, from)
			assert.True(t, errors.Is(err, model.ErrValidation), "%v: %v", from, err)
		}
	})
}

func TestParseSlot(t *testing.T) {
	h := hours(t)
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, h.Loc)

	start, err := h.ParseSlot(now, "2026-11-12", "07:15", 30)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 11, 12, 7, 15, 0, 0, h.Loc), start)

	last, err := h.ParseSlot(now, "2026-11-12", "16:30", 30)
	require.NoError(t, err, "a visit ending exactly at closing fits")
	assert.Equal(t, 16, last.Hour())

	bad := []struct{ name, date, time string }{
		{"bad time format", "2026-11-12", "7am"},
		{"impossible time", "2026-11-12", "25:00"},
		{"off the grid", "2026-11-12", "07:10"},
		{"before opening", "2026-11-12", "06:45"},
		{"runs past closing", "2026-11-12", "16:45"},
		{"today", "2026-11-11", "09:00"},
		{"past the horizon", "2027-02-10", "09:00"},
		{"bad date", "2026-02-30", "09:00"},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.ParseSlot(now, tt.date, tt.time, 30)
			assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
		})
	}
}

func TestFreeSlots(t *testing.T) {
	h := hours(t)
	day := date(2026, 11, 12)
	at := func(hh, mm int) time.Time { return time.Date(2026, 11, 12, hh, mm, 0, 0, h.Loc) }

	t.Run("empty day", func(t *testing.T) {
		slots := h.FreeSlots(day, 30, nil)
		assert.Len(t, slots, 39) // 07:00 .. 16:30
		assert.Equal(t, "07:00", slots[0])
		assert.Equal(t, "16:30", slots[len(slots)-1])
	})

	t.Run("a 45-minute booking blocks its own starts and the ones that would run into it", func(t *testing.T) {
		busy := []model.Interval{{Start: at(9, 0), End: at(9, 45)}}
		slots := h.FreeSlots(day, 30, busy)
		// 08:45 would run until 09:15; 09:00, 09:15 and 09:30 start inside the booking.
		for _, gone := range []string{"08:45", "09:00", "09:15", "09:30"} {
			assert.NotContains(t, slots, gone)
		}
		// 08:30 ends at 09:00 and 09:45 starts at its end: touching is fine.
		assert.Contains(t, slots, "08:30")
		assert.Contains(t, slots, "09:45")
	})

	t.Run("a full day", func(t *testing.T) {
		slots := h.FreeSlots(day, 30, []model.Interval{{Start: at(7, 0), End: at(17, 0)}})
		assert.NotNil(t, slots)
		assert.Empty(t, slots)
	})

	t.Run("daylight-saving changeover keeps local times", func(t *testing.T) {
		// Clocks go back on 2026-11-01 in Chicago.
		slots := h.FreeSlots(date(2026, 11, 1), 15, nil)
		assert.Equal(t, "07:00", slots[0])
		assert.Len(t, slots, 40)
	})
}

func TestDayBounds(t *testing.T) {
	h := hours(t)
	start, end := h.DayBounds(date(2026, 11, 1)) // a 25-hour day
	assert.Equal(t, 25*time.Hour, end.Sub(start))
}
