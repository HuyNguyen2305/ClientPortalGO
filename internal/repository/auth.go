package repository

import (
	"context"
	"fmt"
	"time"

	"clientportal/internal/model"
	"clientportal/internal/repository/db"
)

// Auth stores passwords and login sessions.
type Auth struct {
	db      db.DBTX
	timeout time.Duration
}

// NewAuth returns an auth repository.
func NewAuth(db db.DBTX, timeout time.Duration) *Auth {
	return &Auth{db: db, timeout: timeout}
}

// CreateWithPassword inserts a customer with a password hash. A taken email fails with ErrConflict.
func (r *Auth) CreateWithPassword(ctx context.Context, c model.Customer, hash string) (model.Customer, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out, err := scanCustomer(r.db.QueryRow(ctx,
		`INSERT INTO customers (name, email, phone, password_hash) VALUES ($1, $2, $3, $4) RETURNING `+customerColumns,
		c.Name, c.Email, c.Phone, hash))
	if err != nil {
		return model.Customer{}, fmt.Errorf("insert customer: %w", mapErr(err))
	}
	return out, nil
}

// Credentials returns the customer with this email and their password hash ("" when they have none).
func (r *Auth) Credentials(ctx context.Context, email string) (model.Customer, string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var c model.Customer
	var hash string
	err := r.db.QueryRow(ctx,
		`SELECT `+customerColumns+`, COALESCE(password_hash, '') FROM customers WHERE lower(email) = lower($1)`, email).
		Scan(&c.ID, &c.AccountNumber, &c.Name, &c.Email, &c.Phone, &c.CreatedAt, &c.UpdatedAt, &hash)
	if err != nil {
		return model.Customer{}, "", fmt.Errorf("get credentials: %w", mapErr(err))
	}
	return c, hash, nil
}

// PasswordHash returns a customer's password hash ("" when they have none).
func (r *Auth) PasswordHash(ctx context.Context, id int64) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var hash string
	err := r.db.QueryRow(ctx, `SELECT COALESCE(password_hash, '') FROM customers WHERE id = $1`, id).Scan(&hash)
	if err != nil {
		return "", fmt.Errorf("get password hash of customer %d: %w", id, mapErr(err))
	}
	return hash, nil
}

// CreateSession stores a session and drops that customer's expired ones in the same statement.
// shortcut: no global sweeper, so a customer who never logs in again keeps stale rows; add a cron DELETE if the table grows.
func (r *Auth) CreateSession(ctx context.Context, customerID int64, tokenHash []byte, expiresAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.Exec(ctx,
		`WITH purge AS (DELETE FROM sessions WHERE customer_id = $1 AND expires_at <= now())
		 INSERT INTO sessions (token_hash, customer_id, expires_at) VALUES ($2, $1, $3)`,
		customerID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", mapErr(err))
	}
	return nil
}

// CustomerIDByToken returns the owner of an unexpired session; anything else is ErrNotFound.
func (r *Auth) CustomerIDByToken(ctx context.Context, tokenHash []byte) (int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id int64
	err := r.db.QueryRow(ctx,
		`SELECT customer_id FROM sessions WHERE token_hash = $1 AND expires_at > now()`, tokenHash).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("get session: %w", mapErr(err))
	}
	return id, nil
}

// DeleteSession removes one session; a missing one is not an error.
func (r *Auth) DeleteSession(ctx context.Context, tokenHash []byte) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", mapErr(err))
	}
	return nil
}

// CreateReset stores a reset token, replacing any earlier one for the customer so only the newest link works.
func (r *Auth) CreateReset(ctx context.Context, customerID int64, tokenHash []byte, expiresAt time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.Exec(ctx,
		`WITH purge AS (DELETE FROM password_resets WHERE customer_id = $1)
		 INSERT INTO password_resets (token_hash, customer_id, expires_at) VALUES ($2, $1, $3)`,
		customerID, tokenHash, expiresAt)
	if err != nil {
		return fmt.Errorf("insert password reset: %w", mapErr(err))
	}
	return nil
}

// ConsumeReset spends an unexpired reset token, sets the new hash and ends all of that customer's
// sessions, in one statement so a token works once and nothing is half done. A bad or spent token is ErrNotFound.
func (r *Auth) ConsumeReset(ctx context.Context, tokenHash []byte, hash string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var id int64
	err := r.db.QueryRow(ctx,
		`WITH used AS (DELETE FROM password_resets WHERE token_hash = $1 AND expires_at > now() RETURNING customer_id),
		      upd  AS (UPDATE customers SET password_hash = $2, updated_at = now() WHERE id IN (SELECT customer_id FROM used)),
		      ended AS (DELETE FROM sessions WHERE customer_id IN (SELECT customer_id FROM used))
		 SELECT customer_id FROM used`, tokenHash, hash).Scan(&id)
	if err != nil {
		return fmt.Errorf("consume password reset: %w", mapErr(err))
	}
	return nil
}

// ChangePassword sets a new hash and ends every session except keep, in one statement so both happen or neither.
func (r *Auth) ChangePassword(ctx context.Context, id int64, hash string, keep []byte) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.Exec(ctx,
		`WITH upd AS (UPDATE customers SET password_hash = $2, updated_at = now() WHERE id = $1)
		 DELETE FROM sessions WHERE customer_id = $1 AND token_hash <> $3`, id, hash, keep)
	if err != nil {
		return fmt.Errorf("change password of customer %d: %w", id, mapErr(err))
	}
	return nil
}
