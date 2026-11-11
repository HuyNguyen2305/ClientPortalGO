package model

import "time"

// Customer is a portal account holder.
type Customer struct {
	ID            int64
	AccountNumber int64
	Name          string
	Email         *string
	Phone         *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
