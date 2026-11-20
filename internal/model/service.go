package model

import "time"

// Service statuses. Archived templates are hidden from customers; deleted ones are gone for good
// from the API but kept in the database because bookings reference them.
const (
	ServiceActive   = "active"
	ServiceArchived = "archived"
	ServiceDeleted  = "deleted"
)

// Service is a template a customer can book, defined by an admin.
type Service struct {
	ID              int64
	Name            string
	DurationMinutes int
	PriceCents      int64 // the sum of Items when there are any
	Color           *string
	Status          string
	Recurrence      Recurrence
	SetConfirmed    bool   // false: each job's estimate starts as a draft instead of open
	Items           []Item // invoice lines copied onto each job's estimate; empty means one line of Name and PriceCents
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// EstimateItems are the lines of a job's estimate.
func (s Service) EstimateItems() []Item {
	if len(s.Items) > 0 {
		return s.Items
	}
	return []Item{{Name: s.Name, Quantity: 1, UnitPriceCents: s.PriceCents}}
}
