package dto

import (
	"fmt"
	"time"

	"clientportal/internal/model"
)

const dateLayout = "2006-01-02"

// ItemRequest is one line in a document request. The limits keep totals far from int64 overflow.
type ItemRequest struct {
	Name           string  `json:"name" binding:"required,max=200"`
	Description    *string `json:"description" binding:"omitempty,max=2000"`
	Quantity       int     `json:"quantity" binding:"required,gt=0,lte=1000000"`
	UnitPriceCents *int64  `json:"unit_price_cents" binding:"required,gte=0,lte=10000000000"`
}

// DocumentRequest is the body for creating or replacing an invoice or estimate.
type DocumentRequest struct {
	LocationID   int64         `json:"location_id" binding:"required,gt=0"`
	Title        string        `json:"title" binding:"required,max=200"`
	DocumentDate string        `json:"document_date" binding:"required,datetime=2006-01-02"`
	Items        []ItemRequest `json:"items" binding:"required,min=1,max=100,dive"`
}

// ToInput maps the request to a domain input; positions are the slice indexes.
func (r DocumentRequest) ToInput() (model.DocumentInput, error) {
	date, err := time.Parse(dateLayout, r.DocumentDate)
	if err != nil {
		return model.DocumentInput{}, fmt.Errorf("%w: document_date must be YYYY-MM-DD", model.ErrValidation)
	}
	items := make([]model.Item, len(r.Items))
	for i, it := range r.Items {
		items[i] = model.Item{Position: i, Name: it.Name, Description: it.Description, Quantity: it.Quantity, UnitPriceCents: *it.UnitPriceCents}
	}
	return model.DocumentInput{LocationID: r.LocationID, Title: r.Title, DocumentDate: date, Items: items}, nil
}

// ItemResponse is one line of a document.
type ItemResponse struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	Description    *string `json:"description"`
	Quantity       int     `json:"quantity"`
	UnitPriceCents int64   `json:"unit_price_cents"`
	TotalCents     int64   `json:"total_cents"`
}

// DocumentResponse is an invoice or estimate. Lists omit items; estimates omit the payment fields.
type DocumentResponse struct {
	ID              int64            `json:"id"`
	Number          int64            `json:"number"`
	Status          string           `json:"status"`
	Title           string           `json:"title"`
	DocumentDate    string           `json:"document_date"`
	Location        LocationResponse `json:"location"`
	Items           []ItemResponse   `json:"items,omitempty"`
	SubtotalCents   int64            `json:"subtotal_cents"`
	TotalCents      int64            `json:"total_cents"`
	AmountPaidCents *int64           `json:"amount_paid_cents,omitempty"`
	BalanceDueCents *int64           `json:"balance_due_cents,omitempty"`
}

// FromDocument maps a domain document to its response.
func FromDocument(d model.BillingDocument, withItems bool) DocumentResponse {
	out := DocumentResponse{
		ID:           d.ID,
		Number:       d.Number,
		Status:       string(d.Status),
		Title:        d.Title,
		DocumentDate: d.DocumentDate.Format(dateLayout),
		Location:     FromLocation(d.Location),
		// shortcut: subtotal equals total until tax or discounts exist; split them when they do.
		SubtotalCents: d.TotalCents,
		TotalCents:    d.TotalCents,
	}
	if withItems {
		out.Items = MapSlice(d.Items, func(it model.Item) ItemResponse {
			return ItemResponse{ID: it.ID, Name: it.Name, Description: it.Description, Quantity: it.Quantity,
				UnitPriceCents: it.UnitPriceCents, TotalCents: it.TotalCents()}
		})
	}
	if d.ParentType == model.ParentInvoice {
		paid, due := d.AmountPaidCents, d.BalanceDueCents()
		out.AmountPaidCents, out.BalanceDueCents = &paid, &due
	}
	return out
}

// ListItem maps a document for a list response, without items.
func ListItem(d model.BillingDocument) DocumentResponse { return FromDocument(d, false) }
