// Package repository is the Postgres access layer.
package repository

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"clientportal/internal/model"
)

// mapErr turns driver errors into domain errors; anything else passes through unchanged.
func mapErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503": // foreign_key_violation
			// Postgres words an insert/update into a missing parent differently from a delete of a used one.
			if strings.HasPrefix(pgErr.Message, "insert or update") {
				return fmt.Errorf("%w: a referenced record does not exist", model.ErrValidation)
			}
			return fmt.Errorf("%w: record is still referenced", model.ErrConflict)
		case "23514": // check_violation
			return fmt.Errorf("%w: a value is out of range", model.ErrValidation)
		case "23505": // unique_violation
			return fmt.Errorf("%w: record already exists", model.ErrConflict)
		case "23P01": // exclusion_violation: only bookings_no_overlap today
			return fmt.Errorf("%w: that time slot is no longer available", model.ErrConflict)
		}
	}
	return err
}
