package model

import "time"

// Location is one of a customer's service addresses.
type Location struct {
	ID         int64
	CustomerID int64
	Street     string
	City       string
	State      string
	PostalCode string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
