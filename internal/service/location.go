package service

import (
	"context"
	"fmt"

	"clientportal/internal/model"
)

// LocationRepository is the storage the location service needs. Every method is scoped to a customer.
type LocationRepository interface {
	Create(ctx context.Context, l model.Location) (model.Location, error)
	Get(ctx context.Context, customerID, id int64) (model.Location, error)
	List(ctx context.Context, customerID int64) ([]model.Location, error)
	Update(ctx context.Context, l model.Location) (model.Location, error)
	Delete(ctx context.Context, customerID, id int64) error
}

// LocationService manages a customer's service addresses.
type LocationService struct {
	repo LocationRepository
}

// NewLocationService returns a location service.
func NewLocationService(repo LocationRepository) *LocationService {
	return &LocationService{repo: repo}
}

// cleanAddress trims every address field and requires each to be non-blank.
func cleanAddress(l model.Location) (model.Location, error) {
	var err error
	for _, f := range []struct {
		name string
		v    *string
	}{{"street", &l.Street}, {"city", &l.City}, {"state", &l.State}, {"postal_code", &l.PostalCode}} {
		if *f.v, err = required(f.name, *f.v); err != nil {
			return model.Location{}, err
		}
	}
	return l, nil
}

// Create adds a location for the customer.
func (s *LocationService) Create(ctx context.Context, customerID int64, in model.Location) (model.Location, error) {
	in, err := cleanAddress(in)
	if err != nil {
		return model.Location{}, err
	}
	in.CustomerID = customerID
	l, err := s.repo.Create(ctx, in)
	if err != nil {
		return model.Location{}, fmt.Errorf("create location: %w", err)
	}
	return l, nil
}

// Get returns one of the customer's locations.
func (s *LocationService) Get(ctx context.Context, customerID, id int64) (model.Location, error) {
	l, err := s.repo.Get(ctx, customerID, id)
	if err != nil {
		return model.Location{}, fmt.Errorf("get location: %w", err)
	}
	return l, nil
}

// List returns all of the customer's locations.
func (s *LocationService) List(ctx context.Context, customerID int64) ([]model.Location, error) {
	ls, err := s.repo.List(ctx, customerID)
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}
	return ls, nil
}

// Update replaces the address of one of the customer's locations.
func (s *LocationService) Update(ctx context.Context, customerID, id int64, in model.Location) (model.Location, error) {
	in, err := cleanAddress(in)
	if err != nil {
		return model.Location{}, err
	}
	in.ID, in.CustomerID = id, customerID
	l, err := s.repo.Update(ctx, in)
	if err != nil {
		return model.Location{}, fmt.Errorf("update location: %w", err)
	}
	return l, nil
}

// Delete removes one of the customer's locations that no document references.
func (s *LocationService) Delete(ctx context.Context, customerID, id int64) error {
	if err := s.repo.Delete(ctx, customerID, id); err != nil {
		return fmt.Errorf("delete location: %w", err)
	}
	return nil
}
