package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"clientportal/internal/model"
	"clientportal/internal/service"
	"clientportal/tests/unit/mocks"
)

var authNow = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

const resetURL = "https://portal.test/reset"

func newAuth(repo service.AuthRepository) *service.AuthService {
	return newAuthWithMail(repo, nil)
}

func newAuthWithMail(repo service.AuthRepository, mailer service.Mailer) *service.AuthService {
	return service.NewAuthService(repo, mailer, resetURL, time.Hour, func() time.Time { return authNow })
}

func hashOf(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

func TestAuthService_Register(t *testing.T) {
	tests := []struct {
		name     string
		cname    string
		password string
		wantErr  error
	}{
		{"ok", "Ada", "correct horse", nil},
		{"blank name", "  ", "correct horse", model.ErrValidation},
		{"short password", "Ada", "short", model.ErrValidation},
		{"password over 72 bytes", "Ada", string(make([]byte, 73)), model.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockAuthRepository(t)
			if tt.wantErr == nil {
				repo.EXPECT().CreateWithPassword(mock.Anything, mock.MatchedBy(func(c model.Customer) bool {
					return c.Name == "Ada" && *c.Email == "ada@example.com"
				}), mock.MatchedBy(func(h string) bool {
					return bcrypt.CompareHashAndPassword([]byte(h), []byte(tt.password)) == nil
				})).Return(model.Customer{ID: 4, Name: "Ada"}, nil)
				repo.EXPECT().CreateSession(mock.Anything, int64(4), mock.Anything, authNow.Add(time.Hour)).Return(nil)
			}
			c, token, expires, err := newAuth(repo).Register(context.Background(), tt.cname, "ada@example.com", nil, tt.password)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, int64(4), c.ID)
			assert.Len(t, token, 43) // 32 bytes, base64url without padding
			assert.Equal(t, authNow.Add(time.Hour), expires)
		})
	}
}

func TestAuthService_RegisterDuplicateEmail(t *testing.T) {
	repo := mocks.NewMockAuthRepository(t)
	repo.EXPECT().CreateWithPassword(mock.Anything, mock.Anything, mock.Anything).Return(model.Customer{}, model.ErrConflict)
	_, _, _, err := newAuth(repo).Register(context.Background(), "Ada", "ada@example.com", nil, "correct horse")
	assert.ErrorIs(t, err, model.ErrConflict)
}

func TestAuthService_Login(t *testing.T) {
	good := hashOf(t, "correct horse")
	tests := []struct {
		name     string
		hash     string
		credErr  error
		password string
		wantOK   bool
	}{
		{"right password", good, nil, "correct horse", true},
		{"wrong password", good, nil, "battery staple", false},
		{"unknown email", "", model.ErrNotFound, "correct horse", false},
		{"customer without a password", "", nil, "correct horse", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockAuthRepository(t)
			repo.EXPECT().Credentials(mock.Anything, "ada@example.com").Return(model.Customer{ID: 4}, tt.hash, tt.credErr)
			if tt.wantOK {
				repo.EXPECT().CreateSession(mock.Anything, int64(4), mock.Anything, authNow.Add(time.Hour)).Return(nil)
			}
			_, token, _, err := newAuth(repo).Login(context.Background(), "ada@example.com", tt.password)
			if tt.wantOK {
				require.NoError(t, err)
				assert.NotEmpty(t, token)
				return
			}
			// Every failure looks the same, so the response cannot tell which emails exist.
			assert.ErrorIs(t, err, model.ErrUnauthorized)
			assert.Empty(t, token)
		})
	}
}

func TestAuthService_LoginRepositoryFailure(t *testing.T) {
	boom := errors.New("db down")
	repo := mocks.NewMockAuthRepository(t)
	repo.EXPECT().Credentials(mock.Anything, mock.Anything).Return(model.Customer{}, "", boom)
	_, _, _, err := newAuth(repo).Login(context.Background(), "ada@example.com", "correct horse")
	assert.ErrorIs(t, err, boom)
	assert.NotErrorIs(t, err, model.ErrUnauthorized)
}

func TestAuthService_Authenticate(t *testing.T) {
	tests := []struct {
		name    string
		repoID  int64
		repoErr error
		wantID  int64
		wantErr error
	}{
		{"valid", 4, nil, 4, nil},
		{"unknown or expired", 0, model.ErrNotFound, 0, model.ErrUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockAuthRepository(t)
			repo.EXPECT().CustomerIDByToken(mock.Anything, mock.Anything).Return(tt.repoID, tt.repoErr)
			id, err := newAuth(repo).Authenticate(context.Background(), "tok")
			assert.Equal(t, tt.wantID, id)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestAuthService_Logout(t *testing.T) {
	repo := mocks.NewMockAuthRepository(t)
	repo.EXPECT().DeleteSession(mock.Anything, mock.Anything).Return(nil)
	assert.NoError(t, newAuth(repo).Logout(context.Background(), "tok"))
}

func TestAuthService_ChangePassword(t *testing.T) {
	tests := []struct {
		name    string
		hash    string
		oldPw   string
		newPw   string
		wantErr error
		changes bool
	}{
		{"ok", hashOf(t, "correct horse"), "correct horse", "new password 1", nil, true},
		{"wrong old password", hashOf(t, "correct horse"), "nope nope nope", "new password 1", model.ErrUnauthorized, false},
		{"customer without a password", "", "correct horse", "new password 1", model.ErrUnauthorized, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockAuthRepository(t)
			repo.EXPECT().PasswordHash(mock.Anything, int64(4)).Return(tt.hash, nil)
			if tt.changes {
				repo.EXPECT().ChangePassword(mock.Anything, int64(4), mock.MatchedBy(func(h string) bool {
					return bcrypt.CompareHashAndPassword([]byte(h), []byte(tt.newPw)) == nil
				}), mock.Anything).Return(nil)
			}
			err := newAuth(repo).ChangePassword(context.Background(), 4, "tok", tt.oldPw, tt.newPw)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}

	t.Run("new password too short is rejected before any lookup", func(t *testing.T) {
		err := newAuth(mocks.NewMockAuthRepository(t)).ChangePassword(context.Background(), 4, "tok", "correct horse", "short")
		assert.ErrorIs(t, err, model.ErrValidation)
	})
}

func TestAuthService_RequestReset(t *testing.T) {
	t.Run("unknown email sends nothing and says nothing", func(t *testing.T) {
		repo := mocks.NewMockAuthRepository(t)
		repo.EXPECT().Credentials(mock.Anything, "nobody@example.com").Return(model.Customer{}, "", model.ErrNotFound)
		assert.NoError(t, newAuthWithMail(repo, mocks.NewMockMailer(t)).RequestReset(context.Background(), "nobody@example.com"))
	})

	t.Run("known email stores a token and mails the link", func(t *testing.T) {
		repo := mocks.NewMockAuthRepository(t)
		repo.EXPECT().Credentials(mock.Anything, "ada@example.com").Return(model.Customer{ID: 4}, "", nil)
		repo.EXPECT().CreateReset(mock.Anything, int64(4), mock.Anything, authNow.Add(time.Hour)).Return(nil)
		mailer := mocks.NewMockMailer(t)
		mailer.EXPECT().Send(mock.Anything, "ada@example.com", mock.Anything, mock.MatchedBy(func(body string) bool {
			return strings.Contains(body, resetURL+"?token=")
		})).Return(nil)
		assert.NoError(t, newAuthWithMail(repo, mailer).RequestReset(context.Background(), "ada@example.com"))
	})

	t.Run("a mail failure is swallowed", func(t *testing.T) {
		repo := mocks.NewMockAuthRepository(t)
		repo.EXPECT().Credentials(mock.Anything, mock.Anything).Return(model.Customer{ID: 4}, "", nil)
		repo.EXPECT().CreateReset(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil)
		mailer := mocks.NewMockMailer(t)
		mailer.EXPECT().Send(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(errors.New("provider down"))
		assert.NoError(t, newAuthWithMail(repo, mailer).RequestReset(context.Background(), "ada@example.com"))
	})

	t.Run("a repository failure is returned and nothing is mailed", func(t *testing.T) {
		boom := errors.New("db down")
		repo := mocks.NewMockAuthRepository(t)
		repo.EXPECT().Credentials(mock.Anything, mock.Anything).Return(model.Customer{ID: 4}, "", nil)
		repo.EXPECT().CreateReset(mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(boom)
		err := newAuthWithMail(repo, mocks.NewMockMailer(t)).RequestReset(context.Background(), "ada@example.com")
		assert.ErrorIs(t, err, boom)
	})
}

func TestAuthService_ConfirmReset(t *testing.T) {
	tests := []struct {
		name     string
		password string
		repoErr  error
		callRepo bool
		wantErr  error
	}{
		{"ok", "brand new pass", nil, true, nil},
		{"short password never reaches the repository", "short", nil, false, model.ErrValidation},
		{"bad or spent token", "brand new pass", model.ErrNotFound, true, model.ErrValidation},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := mocks.NewMockAuthRepository(t)
			if tt.callRepo {
				repo.EXPECT().ConsumeReset(mock.Anything, mock.Anything, mock.MatchedBy(func(h string) bool {
					return bcrypt.CompareHashAndPassword([]byte(h), []byte(tt.password)) == nil
				})).Return(tt.repoErr)
			}
			err := newAuth(repo).ConfirmReset(context.Background(), "tok", tt.password)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}
