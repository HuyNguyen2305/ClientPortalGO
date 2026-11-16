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
	// Charge takes amountCents off the saved card at once, with no customer present. Idempotent per key.
	// A decline, or a card that needs extra bank confirmation, is model.ErrPaymentFailed.
	Charge(ctx context.Context, gatewayCustomerID, cardID string, amountCents int64, key string) error
}

// InvoiceBooker is the invoice side of paying: *BillingDocumentService satisfies it.
type InvoiceBooker interface {
	Get(ctx context.Context, customerID int64, pt model.ParentType, id int64) (model.BillingDocument, error)
	RecordPayment(ctx context.Context, customerID, invoiceID, amountCents int64, idempotencyKey string) (model.BillingDocument, error)
	PaymentRecorded(ctx context.Context, customerID, invoiceID int64, key string) (bool, error)
}

// GatewayLinkRepository stores which gateway customer belongs to a portal customer.
type GatewayLinkRepository interface {
	StripeID(ctx context.Context, customerID int64) (string, error)
	SetStripeID(ctx context.Context, customerID int64, stripeID string) error
}

// PaymentMethodService manages a customer's saved cards.
type PaymentMethodService struct {
	links    GatewayLinkRepository
	gw       PaymentGateway
	invoices InvoiceBooker
}

// NewPaymentMethodService returns a payment method service.
func NewPaymentMethodService(links GatewayLinkRepository, gw PaymentGateway, invoices InvoiceBooker) *PaymentMethodService {
	return &PaymentMethodService{links: links, gw: gw, invoices: invoices}
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

// ownCard returns the gateway customer id when cardID is one of the customer's cards, else ErrNotFound.
func (s *PaymentMethodService) ownCard(ctx context.Context, customerID int64, cardID string) (string, error) {
	gid, err := s.links.StripeID(ctx, customerID)
	if err != nil {
		return "", fmt.Errorf("find card: %w", err)
	}
	if gid != "" {
		cards, err := s.gw.ListCards(ctx, gid)
		if err != nil {
			return "", fmt.Errorf("find card: %w", err)
		}
		for _, c := range cards {
			if c.ID == cardID {
				return gid, nil
			}
		}
	}
	return "", fmt.Errorf("find card: %w", model.ErrNotFound)
}

// Delete removes one of the customer's own cards; any other card id is not found.
func (s *PaymentMethodService) Delete(ctx context.Context, customerID int64, cardID string) error {
	if _, err := s.ownCard(ctx, customerID, cardID); err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	if err := s.gw.DetachCard(ctx, cardID); err != nil {
		return fmt.Errorf("detach card: %w", err)
	}
	return nil
}

// PayInvoice charges the invoice's full balance due to one of the customer's saved cards, then books
// the payment. The same key goes to the gateway and to the payment row, so a retry charges and records once.
// shortcut: the amount is read before the invoice is locked, so if the invoice is paid elsewhere in between,
// the charge succeeds but booking fails with ErrConflict (money taken, not booked); add a refund on that path before real money.
func (s *PaymentMethodService) PayInvoice(ctx context.Context, customerID, invoiceID int64, cardID, key string) (model.BillingDocument, error) {
	inv, err := s.invoices.Get(ctx, customerID, model.ParentInvoice, invoiceID)
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("pay invoice: %w", err)
	}
	// A retry of a payment that already went through returns the invoice as it is: no second charge, no error.
	done, err := s.invoices.PaymentRecorded(ctx, customerID, invoiceID, key)
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("pay invoice: %w", err)
	}
	if done {
		return inv, nil
	}
	if inv.Status != model.StatusOpen && inv.Status != model.StatusPartiallyPaid {
		return model.BillingDocument{}, fmt.Errorf("%w: invoice %d is %s and cannot be paid", model.ErrConflict, inv.Number, inv.Status)
	}
	gid, err := s.ownCard(ctx, customerID, cardID)
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("pay invoice: %w", err)
	}
	amount := inv.BalanceDueCents()
	if err := s.gw.Charge(ctx, gid, cardID, amount, key); err != nil {
		return model.BillingDocument{}, fmt.Errorf("charge card: %w", err)
	}
	doc, err := s.invoices.RecordPayment(ctx, customerID, invoiceID, amount, key)
	if err != nil {
		return model.BillingDocument{}, fmt.Errorf("card charged but payment not booked (key %s): %w", key, err)
	}
	return doc, nil
}
