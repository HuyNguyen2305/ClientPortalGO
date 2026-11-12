package model

import "time"

// Service is something a customer can book, defined by an admin.
type Service struct {
	ID              int64
	Name            string
	DurationMinutes int
	PriceCents      int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
