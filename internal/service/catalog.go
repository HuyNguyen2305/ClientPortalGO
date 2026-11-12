package service

import (
	"context"
	"fmt"

	"clientportal/internal/model"
)

// ServiceRepository stores the bookable services.
type ServiceRepository interface {
	Create(ctx context.Context, s model.Service) (model.Service, error)
	Get(ctx context.Context, id int64) (model.Service, error)
	List(ctx context.Context) ([]model.Service, error)
	Update(ctx context.Context, s model.Service) (model.Service, error)
	Delete(ctx context.Context, id int64) error
}

// Catalog manages the services customers can book. (Named to avoid service.ServiceService.)
type Catalog struct {
	repo ServiceRepository
}

// NewCatalog returns a catalog.
func NewCatalog(repo ServiceRepository) *Catalog {
	return &Catalog{repo: repo}
}

func cleanService(s model.Service) (model.Service, error) {
	name, err := required("name", s.Name)
	if err != nil {
		return s, err
	}
	s.Name = name
	if s.DurationMinutes < 15 || s.DurationMinutes > 480 || s.DurationMinutes%15 != 0 {
		return s, fmt.Errorf("%w: duration_minutes must be a multiple of 15 between 15 and 480", model.ErrValidation)
	}
	return s, nil
}

// Create adds a service.
func (c *Catalog) Create(ctx context.Context, s model.Service) (model.Service, error) {
	s, err := cleanService(s)
	if err != nil {
		return model.Service{}, err
	}
	out, err := c.repo.Create(ctx, s)
	if err != nil {
		return model.Service{}, fmt.Errorf("create service: %w", err)
	}
	return out, nil
}

// Get returns one service.
func (c *Catalog) Get(ctx context.Context, id int64) (model.Service, error) {
	s, err := c.repo.Get(ctx, id)
	if err != nil {
		return model.Service{}, fmt.Errorf("get service: %w", err)
	}
	return s, nil
}

// List returns every service.
func (c *Catalog) List(ctx context.Context) ([]model.Service, error) {
	ss, err := c.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	return ss, nil
}

// Update replaces a service. Existing bookings keep the times they were booked with.
func (c *Catalog) Update(ctx context.Context, id int64, s model.Service) (model.Service, error) {
	s, err := cleanService(s)
	if err != nil {
		return model.Service{}, err
	}
	s.ID = id
	out, err := c.repo.Update(ctx, s)
	if err != nil {
		return model.Service{}, fmt.Errorf("update service: %w", err)
	}
	return out, nil
}

// Delete removes a service that no booking uses.
func (c *Catalog) Delete(ctx context.Context, id int64) error {
	if err := c.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}
