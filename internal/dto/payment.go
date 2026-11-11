package dto

import (
	"time"

	"clientportal/internal/model"
)

// PaymentRequest is the body for recording a payment.
type PaymentRequest struct {
	AmountCents int64 `json:"amount_cents" binding:"required,gt=0"`
}

// PaymentResponse is a payment as returned by the API.
type PaymentResponse struct {
	ID          int64     `json:"id"`
	AmountCents int64     `json:"amount_cents"`
	PaidAt      time.Time `json:"paid_at"`
}

// FromPayment maps a domain payment to its response.
func FromPayment(p model.Payment) PaymentResponse {
	return PaymentResponse{ID: p.ID, AmountCents: p.AmountCents, PaidAt: p.PaidAt}
}

// BalanceResponse is the customer's account balance.
type BalanceResponse struct {
	BalanceCents int64 `json:"balance_cents"`
}
