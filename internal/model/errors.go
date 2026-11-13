// Package model holds the domain types and the rules that belong to them.
package model

import "errors"

// Domain errors. Services wrap them with context; the handler maps them to HTTP codes.
var (
	ErrNotFound   = errors.New("not found")
	ErrConflict   = errors.New("conflict")
	ErrValidation = errors.New("invalid request")

	ErrUnauthorized = errors.New("unauthorized")
)
