package dto

import (
	"fmt"
	"time"

	"clientportal/internal/model"
)

// RepeatRequest is a template's repeat rule. Every field is optional: no unit means no repeat.
type RepeatRequest struct {
	Unit      string  `json:"unit" binding:"omitempty,oneof=off daily weekly monthly yearly"`
	Every     int     `json:"every" binding:"omitempty,gte=1,lte=1000"`
	By        string  `json:"by" binding:"omitempty,oneof=weekday monthday"`
	Ends      string  `json:"ends" binding:"omitempty,oneof=never after on_date"`
	EndsAfter int     `json:"ends_after" binding:"omitempty,gte=1,lte=1000"`
	EndsOn    *string `json:"ends_on" binding:"omitempty,datetime=2006-01-02"`
}

// ServiceRequest is the body for creating or updating a service template. When items are given,
// price_cents is ignored and the template's price is the sum of the items.
type ServiceRequest struct {
	Name            string        `json:"name" binding:"required,max=200"`
	DurationMinutes int           `json:"duration_minutes" binding:"required"`
	PriceCents      *int64        `json:"price_cents" binding:"required,gte=0,lte=10000000000"`
	Color           *string       `json:"color" binding:"omitempty,len=7,hexcolor"`
	Repeat          RepeatRequest `json:"repeat"`
	SetConfirmed    *bool         `json:"set_confirmed"` // default true
	Items           []ItemRequest `json:"items" binding:"omitempty,max=100,dive"`
}

// ToModel maps the request to a domain service.
func (r ServiceRequest) ToModel() (model.Service, error) {
	rep := model.Recurrence{Unit: r.Repeat.Unit, Every: r.Repeat.Every, By: r.Repeat.By,
		EndsKind: r.Repeat.Ends, EndsAfter: r.Repeat.EndsAfter}
	if r.Repeat.EndsOn != nil {
		d, err := time.Parse(time.DateOnly, *r.Repeat.EndsOn)
		if err != nil {
			return model.Service{}, fmt.Errorf("%w: ends_on must be YYYY-MM-DD", model.ErrValidation)
		}
		rep.EndsOn = &d
	}
	items := make([]model.Item, len(r.Items))
	for i, it := range r.Items {
		items[i] = model.Item{Name: it.Name, Description: it.Description, Quantity: it.Quantity, UnitPriceCents: *it.UnitPriceCents}
	}
	return model.Service{
		Name: r.Name, DurationMinutes: r.DurationMinutes, PriceCents: *r.PriceCents, Color: r.Color,
		Recurrence: rep, SetConfirmed: r.SetConfirmed == nil || *r.SetConfirmed, Items: items,
	}, nil
}

// RepeatResponse is a template's repeat rule.
type RepeatResponse struct {
	Unit      string  `json:"unit"`
	Every     int     `json:"every"`
	By        string  `json:"by,omitempty"`
	Ends      string  `json:"ends"`
	EndsAfter int     `json:"ends_after,omitempty"`
	EndsOn    *string `json:"ends_on,omitempty"`
}

// ServiceResponse is a service template as returned by the catalog.
type ServiceResponse struct {
	ID              int64          `json:"id"`
	Name            string         `json:"name"`
	DurationMinutes int            `json:"duration_minutes"`
	PriceCents      int64          `json:"price_cents"`
	Color           *string        `json:"color"`
	Status          string         `json:"status"`
	Repeat          RepeatResponse `json:"repeat"`
	SetConfirmed    bool           `json:"set_confirmed"`
	Items           []ItemResponse `json:"items"`
}

// FromService maps a domain service to its response.
func FromService(s model.Service) ServiceResponse {
	r := s.Recurrence
	rep := RepeatResponse{Unit: r.Unit, Every: r.Every, By: r.By, Ends: r.EndsKind, EndsAfter: r.EndsAfter}
	if r.EndsOn != nil {
		d := r.EndsOn.Format(time.DateOnly)
		rep.EndsOn = &d
	}
	return ServiceResponse{
		ID: s.ID, Name: s.Name, DurationMinutes: s.DurationMinutes, PriceCents: s.PriceCents, Color: s.Color,
		Status: s.Status, Repeat: rep, SetConfirmed: s.SetConfirmed,
		Items: MapSlice(s.Items, func(it model.Item) ItemResponse {
			return ItemResponse{ID: it.ID, Name: it.Name, Description: it.Description, Quantity: it.Quantity,
				UnitPriceCents: it.UnitPriceCents, TotalCents: it.TotalCents()}
		}),
	}
}

// ServiceSummary is the part of a service a booking shows.
type ServiceSummary struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	DurationMinutes int    `json:"duration_minutes"`
	PriceCents      int64  `json:"price_cents"`
}
