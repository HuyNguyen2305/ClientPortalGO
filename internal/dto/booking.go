package dto

import (
	"time"

	"clientportal/internal/model"
)

// BookingRequest is the body for confirming a booking. Date and time are business-local.
type BookingRequest struct {
	ServiceID  int64  `json:"service_id" binding:"required,gt=0"`
	LocationID int64  `json:"location_id" binding:"required,gt=0"`
	Date       string `json:"date" binding:"required"`
	Time       string `json:"time" binding:"required"`
}

// ToInput maps the request to a domain input.
func (r BookingRequest) ToInput() model.BookingInput {
	return model.BookingInput{ServiceID: r.ServiceID, LocationID: r.LocationID, Date: r.Date, Time: r.Time}
}

// BookingEstimate is the estimate a booking created.
type BookingEstimate struct {
	ID         int64  `json:"id"`
	Number     int64  `json:"number"`
	Status     string `json:"status"`
	TotalCents int64  `json:"total_cents"`
}

// BookingResponse is a booking as returned by the API.
type BookingResponse struct {
	ID        int64            `json:"id"`
	Service   ServiceResponse  `json:"service"`
	Location  LocationResponse `json:"location"`
	Date      string           `json:"date"`
	Time      string           `json:"time"`
	StartsAt  time.Time        `json:"starts_at"`
	EndsAt    time.Time        `json:"ends_at"`
	Cancelled bool             `json:"cancelled"`
	Estimate  BookingEstimate  `json:"estimate"`
}

// FromBooking maps a booking, showing its times in the business timezone loc.
func FromBooking(b model.Booking, loc *time.Location) BookingResponse {
	start := b.StartsAt.In(loc)
	return BookingResponse{
		ID:        b.ID,
		Service:   FromService(b.Service),
		Location:  FromLocation(b.Location),
		Date:      start.Format(time.DateOnly),
		Time:      start.Format("15:04"),
		StartsAt:  start,
		EndsAt:    b.EndsAt.In(loc),
		Cancelled: b.CancelledAt != nil,
		Estimate: BookingEstimate{ID: b.Estimate.ID, Number: b.Estimate.Number,
			Status: string(b.Estimate.Status), TotalCents: b.Estimate.TotalCents},
	}
}

// Day is one entry of the day picker.
type Day struct {
	Date    string `json:"date"`
	Weekday string `json:"weekday"`
}

// DaysResponse is one page of the day picker; Previous and Next are absent at the edges.
type DaysResponse struct {
	Days     []Day   `json:"days"`
	Previous *string `json:"previous,omitempty"`
	Next     *string `json:"next,omitempty"`
}

// FromDays maps a page of dates.
func FromDays(days []time.Time, prev, next *time.Time) DaysResponse {
	format := func(t *time.Time) *string {
		if t == nil {
			return nil
		}
		s := t.Format(time.DateOnly)
		return &s
	}
	return DaysResponse{
		Days: MapSlice(days, func(d time.Time) Day {
			return Day{Date: d.Format(time.DateOnly), Weekday: d.Weekday().String()}
		}),
		Previous: format(prev),
		Next:     format(next),
	}
}

// SlotsResponse lists the free start times on a date.
type SlotsResponse struct {
	Date  string   `json:"date"`
	Slots []string `json:"slots"`
}
