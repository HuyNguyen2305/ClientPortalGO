package dto

import "clientportal/internal/model"

// ServiceRequest is the body for creating or updating a service.
type ServiceRequest struct {
	Name            string `json:"name" binding:"required,max=200"`
	DurationMinutes int    `json:"duration_minutes" binding:"required"`
	PriceCents      *int64 `json:"price_cents" binding:"required,gte=0,lte=10000000000"`
}

// ToModel maps the request to a domain service.
func (r ServiceRequest) ToModel() model.Service {
	return model.Service{Name: r.Name, DurationMinutes: r.DurationMinutes, PriceCents: *r.PriceCents}
}

// ServiceResponse is a service as returned by the API.
type ServiceResponse struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	DurationMinutes int    `json:"duration_minutes"`
	PriceCents      int64  `json:"price_cents"`
}

// FromService maps a domain service to its response.
func FromService(s model.Service) ServiceResponse {
	return ServiceResponse{ID: s.ID, Name: s.Name, DurationMinutes: s.DurationMinutes, PriceCents: s.PriceCents}
}
