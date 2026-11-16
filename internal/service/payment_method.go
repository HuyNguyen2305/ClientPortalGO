package service

import (
	"context"
	"fmt"

	"clientportal/internal/model"
)

// PaymentGateway is the card processor (Stripe). Cards are entered in the browser, never sent here.
type PaymentGateway interface {
	// CreateCustomer must be idempotent per customerID: calling it twice returns the same id.
	CreateCustomer(ctx context.Context, customerID int64) (string, error)
	CreateSetupIntent(ctx context.Context, gatewayCustomerID string) (clientSecret string, err error)
	ListCards(ctx context.Context, gatewayCustomerID string) ([]model.Card, error)
	DetachCard(ctx context.Context, cardID string) error
}

// GatewayLinkRepository stores which gateway customer belongs to a portal customer.
type GatewayLinkRepository interface {
	StripeID(ctx context.Context, customerID int64) (string, error)
	SetStripeID(ctx context.Context, customerID int64, stripeID string) error
}

// PaymentMethodService manages a customer's saved cards.
type PaymentMethodService struct {
	links GatewayLinkRepository
	gw    PaymentGateway
}

// NewPaymentMethodService returns a payment method service.
func NewPaymentMethodService(links GatewayLinkRepository, gw PaymentGateway) *PaymentMethodService {
	return &PaymentMethodService{links: links, gw: gw}
}

// SetupIntent starts saving a card and returns the secret Stripe.js confirms it with.
func (s *PaymentMethodService) SetupIntent(ctx context.Context, customerID int64) (string, error) {
	gid, err := s.links.StripeID(ctx, customerID)
	if err != nil {
		return "", fmt.Errorf("setup intent: %w", err)
	}
	if gid == "" {
		if gid, err = s.gw.CreateCustomer(ctx, customerID); err != nil {
			return "", fmt.Errorf("create gateway customer: %w", err)
		}
		if err = s.links.SetStripeID(ctx, customerID, gid); err != nil {
			return "", fmt.Errorf("link gateway customer: %w", err)
		}
	}
	secret, err := s.gw.CreateSetupIntent(ctx, gid)
	if err != nil {
		return "", fmt.Errorf("create setup intent: %w", err)
	}
	return secret, nil
}

// List returns the customer's saved cards. shortcut: first page only (Stripe's default 10 cards), upgrade when a customer needs more.
func (s *PaymentMethodService) List(ctx context.Context, customerID int64) ([]model.Card, error) {
	gid, err := s.links.StripeID(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	if gid == "" {
		return []model.Card{}, nil
	}
	cards, err := s.gw.ListCards(ctx, gid)
	if err != nil {
		return nil, fmt.Errorf("list cards: %w", err)
	}
	return cards, nil
}

// Delete removes one of the customer's own cards; any other card id is not found.
func (s *PaymentMethodService) Delete(ctx context.Context, customerID int64, cardID string) error {
	cards, err := s.List(ctx, customerID)
	if err != nil {
		return err
	}
	for _, c := range cards {
		if c.ID == cardID {
			if err := s.gw.DetachCard(ctx, cardID); err != nil {
				return fmt.Errorf("detach card: %w", err)
			}
			return nil
		}
	}
	return fmt.Errorf("delete card: %w", model.ErrNotFound)
}
