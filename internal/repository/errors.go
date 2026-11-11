// Package repository is the Postgres access layer.
package repository

import (
	"errors"
	"fmt"

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
			return fmt.Errorf("%w: record is still referenced", model.ErrConflict)
		case "23505": // unique_violation
			return fmt.Errorf("%w: record already exists", model.ErrConflict)
		}
	}
	return err
}
