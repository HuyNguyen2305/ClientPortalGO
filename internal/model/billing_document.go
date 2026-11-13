package model

import (
	"slices"
	"time"
)

// ParentType distinguishes the two kinds of row in billing_documents.
type ParentType string

// Parent types.
const (
	ParentInvoice  ParentType = "invoice"
	ParentEstimate ParentType = "estimate"
)

// Status is a billing document's lifecycle state.
type Status string

// Statuses. Which ones apply depends on the parent type; see transitions.
const (
	StatusDraft         Status = "draft"
	StatusOpen          Status = "open"
	StatusPartiallyPaid Status = "partially_paid"
	StatusPaid          Status = "paid"
	StatusVoid          Status = "void"
	StatusAccepted      Status = "accepted"
	StatusDeclined      Status = "declined"
)

// transitions lists, per parent type, every status and the statuses it may move to.
// A status with no outgoing edges still appears so ValidStatus can find it.
// A partially paid invoice cannot be voided on purpose: there is no refund flow yet, so voiding
// would leave the money received unaccounted for. Add the edge together with refunds.
var transitions = map[ParentType]map[Status][]Status{
	ParentInvoice: {
		StatusDraft: {StatusOpen, StatusVoid},
		StatusOpen:  {StatusVoid, StatusPartiallyPaid, StatusPaid},
		// A second partial payment keeps the invoice partially paid.
		StatusPartiallyPaid: {StatusPartiallyPaid, StatusPaid},
		StatusPaid:          nil,
		StatusVoid:          nil,
	},
	ParentEstimate: {
		StatusDraft:    {StatusOpen},
		StatusOpen:     {StatusAccepted, StatusDeclined},
		StatusAccepted: nil,
		StatusDeclined: nil,
	},
}

// CanTransition reports whether a document of type pt may move from one status to another.
func CanTransition(pt ParentType, from, to Status) bool {
	return slices.Contains(transitions[pt][from], to)
}

// ValidStatus reports whether s is a status that documents of type pt can have.
func ValidStatus(pt ParentType, s Status) bool {
	_, ok := transitions[pt][s]
	return ok
}

// Item is one line on an invoice or estimate.
type Item struct {
	ID                int64
	BillingDocumentID int64
	Position          int
	Name              string
	Description       *string
	Quantity          int
	UnitPriceCents    int64
}

// TotalCents is the line total.
func (i Item) TotalCents() int64 { return int64(i.Quantity) * i.UnitPriceCents }

// SumItems is the total of all lines.
func SumItems(items []Item) int64 {
	var sum int64
	for _, it := range items {
		sum += it.TotalCents()
	}
	return sum
}

// BillingDocument is an invoice or an estimate.
type BillingDocument struct {
	ID              int64
	ParentType      ParentType
	Number          int64
	CustomerID      int64
	Location        Location // ID always set; address fields set by reads
	Title           string
	Status          Status
	DocumentDate    time.Time
	TotalCents      int64
	AmountPaidCents int64
	ConvertedFromID *int64
	Items           []Item
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// BalanceDueCents is what remains to be paid.
func (d BillingDocument) BalanceDueCents() int64 { return d.TotalCents - d.AmountPaidCents }

// StatusAfterPayment is the invoice status once paidCents of totalCents has been paid.
func StatusAfterPayment(totalCents, paidCents int64) Status {
	if paidCents >= totalCents {
		return StatusPaid
	}
	return StatusPartiallyPaid
}

// Payment is money received against an invoice.
type Payment struct {
	ID                int64
	BillingDocumentID int64
	AmountCents       int64
	PaidAt            time.Time
}

// DocumentInput is what a caller supplies to create or replace a document.
type DocumentInput struct {
	LocationID   int64
	Title        string
	DocumentDate time.Time
	Items        []Item
}

// DocumentFilter selects one page of a customer's documents.
type DocumentFilter struct {
	CustomerID int64
	ParentType ParentType
	LocationID *int64
	Status     *Status
	After      *DocumentCursor
	Limit      int
}
