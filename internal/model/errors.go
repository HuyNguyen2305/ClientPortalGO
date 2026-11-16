// Package model holds the domain types and the rules that belong to them.
package model

import "errors"

// Domain errors. Services wrap them with context; the handler maps them to HTTP codes.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrValidation = errors.New("invalid request")

	ErrUnauthorized = errors.New("unauthorized")

	// ErrPaymentFailed is a card charge that did not go through: declined, or needing extra bank confirmation.
	ErrPaymentFailed = errors.New("payment failed")
)
