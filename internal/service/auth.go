package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"clientportal/internal/model"
)

const (
	bcryptCost     = 12
	minPasswordLen = 10
	maxPasswordLen = 72 // bcrypt ignores or rejects anything longer
)

// AuthRepository is the storage the auth service needs.
type AuthRepository interface {
	CreateWithPassword(ctx context.Context, c model.Customer, hash string) (model.Customer, error)
	Credentials(ctx context.Context, email string) (model.Customer, string, error)
	PasswordHash(ctx context.Context, id int64) (string, error)
	CreateSession(ctx context.Context, customerID int64, tokenHash []byte, expiresAt time.Time) error
	CustomerIDByToken(ctx context.Context, tokenHash []byte) (int64, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	ChangePassword(ctx context.Context, id int64, hash string, keep []byte) error
}

// AuthService registers customers and manages their logins.
type AuthService struct {
	repo AuthRepository
	ttl  time.Duration
	now  func() time.Time
}

// NewAuthService returns an auth service whose sessions last ttl.
func NewAuthService(repo AuthRepository, ttl time.Duration, now func() time.Time) *AuthService {
	return &AuthService{repo: repo, ttl: ttl, now: now}
}

var errBadLogin = fmt.Errorf("%w: invalid credentials", model.ErrUnauthorized)

// dummyHash is compared when no real hash exists, so an unknown email costs the same time as a wrong password.
var dummyHash = sync.OnceValue(func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcryptCost)
	if err != nil {
		panic(err)
	}
	return h
})

func checkPassword(pw string) error {
	if len(pw) < minPasswordLen || len(pw) > maxPasswordLen {
		return fmt.Errorf("%w: password must be %d to %d bytes", model.ErrValidation, minPasswordLen, maxPasswordLen)
	}
	return nil
}

func hashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// startSession stores a new session and returns its token and expiry.
func (s *AuthService) startSession(ctx context.Context, customerID int64) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	expires := s.now().Add(s.ttl)
	if err := s.repo.CreateSession(ctx, customerID, hashToken(token), expires); err != nil {
		return "", time.Time{}, fmt.Errorf("start session: %w", err)
	}
	return token, expires, nil
}

// Register creates a customer with a password and logs them in.
// shortcut: a taken email answers 409, which reveals that the address is registered; upgrade with email verification.
func (s *AuthService) Register(ctx context.Context, name, email string, phone *string, password string) (model.Customer, string, time.Time, error) {
	name, err := required("name", name)
	if err != nil {
		return model.Customer{}, "", time.Time{}, err
	}
	if err := checkPassword(password); err != nil {
		return model.Customer{}, "", time.Time{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return model.Customer{}, "", time.Time{}, fmt.Errorf("hash password: %w", err)
	}
	c, err := s.repo.CreateWithPassword(ctx, model.Customer{Name: name, Email: &email, Phone: phone}, string(hash))
	if err != nil {
		return model.Customer{}, "", time.Time{}, fmt.Errorf("register: %w", err)
	}
	token, expires, err := s.startSession(ctx, c.ID)
	if err != nil {
		return model.Customer{}, "", time.Time{}, err
	}
	return c, token, expires, nil
}

// Login checks the password and starts a session. Every failure is the same ErrUnauthorized.
func (s *AuthService) Login(ctx context.Context, email, password string) (model.Customer, string, time.Time, error) {
	c, hash, err := s.repo.Credentials(ctx, email)
	switch {
	case errors.Is(err, model.ErrNotFound):
		hash = ""
	case err != nil:
		return model.Customer{}, "", time.Time{}, fmt.Errorf("login: %w", err)
	}
	stored := []byte(hash)
	if hash == "" {
		stored = dummyHash()
	}
	if bcrypt.CompareHashAndPassword(stored, []byte(password)) != nil || hash == "" {
		return model.Customer{}, "", time.Time{}, errBadLogin
	}
	token, expires, err := s.startSession(ctx, c.ID)
	if err != nil {
		return model.Customer{}, "", time.Time{}, err
	}
	return c, token, expires, nil
}

// Authenticate returns the customer a token belongs to; a bad or expired token is ErrUnauthorized.
func (s *AuthService) Authenticate(ctx context.Context, token string) (int64, error) {
	id, err := s.repo.CustomerIDByToken(ctx, hashToken(token))
	if errors.Is(err, model.ErrNotFound) {
		return 0, fmt.Errorf("%w: invalid or expired token", model.ErrUnauthorized)
	}
	if err != nil {
		return 0, fmt.Errorf("authenticate: %w", err)
	}
	return id, nil
}

// Logout ends the session the token belongs to.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	if err := s.repo.DeleteSession(ctx, hashToken(token)); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// ChangePassword replaces a customer's password and ends all their other sessions.
func (s *AuthService) ChangePassword(ctx context.Context, id int64, token, oldPassword, newPassword string) error {
	if err := checkPassword(newPassword); err != nil {
		return err
	}
	hash, err := s.repo.PasswordHash(ctx, id)
	if err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	if hash == "" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)) != nil {
		return errBadLogin
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := s.repo.ChangePassword(ctx, id, string(newHash), hashToken(token)); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	return nil
}
