package dto

import "clientportal/internal/model"

// LocationRequest is the body for creating or updating a location.
type LocationRequest struct {
	Street     string `json:"street" binding:"required,max=200"`
	City       string `json:"city" binding:"required,max=200"`
	State      string `json:"state" binding:"required,max=200"`
	PostalCode string `json:"postal_code" binding:"required,max=200"`
}

// ToModel maps the request to a domain location.
func (r LocationRequest) ToModel() model.Location {
	return model.Location{Street: r.Street, City: r.City, State: r.State, PostalCode: r.PostalCode}
}

// LocationResponse is a location as returned by the API.
type LocationResponse struct {
	ID         int64  `json:"id"`
	Street     string `json:"street"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
}

// FromLocation maps a domain location to its response.
func FromLocation(l model.Location) LocationResponse {
	return LocationResponse{ID: l.ID, Street: l.Street, City: l.City, State: l.State, PostalCode: l.PostalCode}
}
