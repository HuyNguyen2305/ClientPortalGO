package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clientportal/internal/model"
)

func day(s string) time.Time {
	d, err := model.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func TestRecurrenceDays(t *testing.T) {
	on := func(s string) *time.Time { d := day(s); return &d }
	tests := []struct {
		name        string
		r           model.Recurrence
		first, last string
		want        []string
	}{
		{"off is one job", model.Recurrence{Unit: model.RepeatOff}, "2026-12-10", "2027-03-10", []string{"2026-12-10"}},
		{"first after last", model.Recurrence{Unit: model.RepeatDaily, Every: 1}, "2026-12-10", "2026-12-09", nil},
		{"daily every 2", model.Recurrence{Unit: model.RepeatDaily, Every: 2}, "2026-12-10", "2026-12-15",
			[]string{"2026-12-10", "2026-12-12", "2026-12-14"}},
		{"weekly", model.Recurrence{Unit: model.RepeatWeekly, Every: 1}, "2026-12-10", "2026-12-31",
			[]string{"2026-12-10", "2026-12-17", "2026-12-24", "2026-12-31"}},
		{"monthly by day of month", model.Recurrence{Unit: model.RepeatMonthly, Every: 1, By: model.ByMonthday}, "2026-12-10", "2027-02-28",
			[]string{"2026-12-10", "2027-01-10", "2027-02-10"}},
		{"monthly defaults to day of month", model.Recurrence{Unit: model.RepeatMonthly, Every: 2}, "2026-12-10", "2027-04-30",
			[]string{"2026-12-10", "2027-02-10", "2027-04-10"}},
		{"monthly on the 31st skips short months", model.Recurrence{Unit: model.RepeatMonthly, Every: 1}, "2026-12-31", "2027-04-30",
			[]string{"2026-12-31", "2027-01-31", "2027-03-31"}},
		{"monthly on the 3rd Friday", model.Recurrence{Unit: model.RepeatMonthly, Every: 1, By: model.ByWeekday}, "2026-12-18", "2027-02-28",
			[]string{"2026-12-18", "2027-01-15", "2027-02-19"}},
		{"monthly on a 5th Thursday skips months without one", model.Recurrence{Unit: model.RepeatMonthly, Every: 1, By: model.ByWeekday}, "2026-12-31", "2027-03-31",
			[]string{"2026-12-31"}},
		{"yearly", model.Recurrence{Unit: model.RepeatYearly, Every: 1}, "2026-12-10", "2028-12-10",
			[]string{"2026-12-10", "2027-12-10", "2028-12-10"}},
		{"yearly 29 February skips non-leap years", model.Recurrence{Unit: model.RepeatYearly, Every: 1}, "2028-02-29", "2033-01-01",
			[]string{"2028-02-29", "2032-02-29"}},
		{"ends after N counts real jobs", model.Recurrence{Unit: model.RepeatMonthly, Every: 1, EndsKind: model.EndsAfter, EndsAfter: 2}, "2026-12-31", "2027-12-31",
			[]string{"2026-12-31", "2027-01-31"}},
		{"ends on date", model.Recurrence{Unit: model.RepeatWeekly, Every: 1, EndsKind: model.EndsOnDate, EndsOn: on("2026-12-20")}, "2026-12-10", "2027-03-10",
			[]string{"2026-12-10", "2026-12-17"}},
		{"ends on date after last is capped by last", model.Recurrence{Unit: model.RepeatDaily, Every: 1, EndsKind: model.EndsOnDate, EndsOn: on("2027-03-10")}, "2026-12-10", "2026-12-11",
			[]string{"2026-12-10", "2026-12-11"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var want []time.Time
			for _, s := range tt.want {
				want = append(want, day(s))
			}
			assert.Equal(t, want, tt.r.Days(day(tt.first), day(tt.last)))
		})
	}
}
