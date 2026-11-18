package dto

import (
	"time"

	"clientportal/internal/model"
)

// RegisterRequest is the body for creating an account. The password length is checked in the service, in bytes.
type RegisterRequest struct {
	Name     string  `json:"name" binding:"required,max=200"`
	Email    string  `json:"email" binding:"required,email,max=320"`
	Phone    *string `json:"phone" binding:"omitempty,max=50"`
	Password string  `json:"password" binding:"required"`
}

// LoginRequest is the body for logging in.
type LoginRequest struct {
	Email    string `json:"email" binding:"required,max=320"`
	Password string `json:"password" binding:"required"`
}

// ChangePasswordRequest is the body for changing the password.
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required"`
}

// AuthResponse is what register and login return: the bearer token and who it belongs to.
type AuthResponse struct {
	Token     string           `json:"token"`
	ExpiresAt time.Time        `json:"expires_at"`
	Customer  CustomerResponse `json:"customer"`
}

// FromAuth builds the login response.
func FromAuth(c model.Customer, token string, expiresAt time.Time) AuthResponse {
	return AuthResponse{Token: token, ExpiresAt: expiresAt, Customer: FromCustomer(c)}
}
