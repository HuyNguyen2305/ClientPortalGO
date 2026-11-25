package service

import (
	"context"
	"fmt"

	"clientportal/internal/model"
)

// ServiceRepository stores the bookable service templates.
type ServiceRepository interface {
	Create(ctx context.Context, s model.Service) (model.Service, error)
	// Get returns a service of any status except deleted.
	Get(ctx context.Context, id int64) (model.Service, error)
	// List returns the services with the given status.
	List(ctx context.Context, status string) ([]model.Service, error)
	Update(ctx context.Context, s model.Service) (model.Service, error)
	// SetStatus archives, restores or soft-deletes a service.
	SetStatus(ctx context.Context, id int64, status string) error
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
	if s.Recurrence, err = cleanRecurrence(s.Recurrence); err != nil {
		return s, err
	}
	if len(s.Items) > 0 {
		s.PriceCents = model.SumItems(s.Items)
	}
	return s, nil
}

// cleanRecurrence fills the defaults and rejects combinations the database would refuse.
func cleanRecurrence(r model.Recurrence) (model.Recurrence, error) {
	if r.Unit == "" {
		r.Unit = model.RepeatOff
	}
	if r.Every == 0 {
		r.Every = 1
	}
	if r.EndsKind == "" {
		r.EndsKind = model.EndsNever
	}
	if r.Unit == model.RepeatMonthly && r.By == "" {
		r.By = model.ByMonthday
	}
	switch {
	case r.Every < 1:
		return r, fmt.Errorf("%w: repeat every must be at least 1", model.ErrValidation)
	case r.By != "" && r.Unit != model.RepeatMonthly:
		return r, fmt.Errorf("%w: repeat_by applies to monthly repeats only", model.ErrValidation)
	case r.EndsKind == model.EndsAfter && r.EndsAfter < 1:
		return r, fmt.Errorf("%w: ends_after must be at least 1", model.ErrValidation)
	case r.EndsKind == model.EndsOnDate && r.EndsOn == nil:
		return r, fmt.Errorf("%w: ends_on is required when the series ends on a date", model.ErrValidation)
	}
	if r.EndsKind != model.EndsAfter {
		r.EndsAfter = 0
	}
	if r.EndsKind != model.EndsOnDate {
		r.EndsOn = nil
	}
	return r, nil
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

// GetActive returns a service for customers: an archived or deleted one is not found.
func (c *Catalog) GetActive(ctx context.Context, id int64) (model.Service, error) {
	s, err := c.repo.Get(ctx, id)
	if err != nil {
		return model.Service{}, fmt.Errorf("get service: %w", err)
	}
	if s.Status != model.ServiceActive {
		return model.Service{}, fmt.Errorf("get service %d: %w", id, model.ErrNotFound)
	}
	return s, nil
}

// List returns the services with the given status; "" means active.
func (c *Catalog) List(ctx context.Context, status string) ([]model.Service, error) {
	switch status {
	case "":
		status = model.ServiceActive
	case model.ServiceActive, model.ServiceArchived, model.ServiceDeleted:
	default:
		return nil, fmt.Errorf("%w: status must be active, archived or deleted", model.ErrValidation)
	}
	ss, err := c.repo.List(ctx, status)
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

// Archive hides a service from customers; bookings and series already made are untouched.
func (c *Catalog) Archive(ctx context.Context, id int64) error {
	if err := c.repo.SetStatus(ctx, id, model.ServiceArchived); err != nil {
		return fmt.Errorf("archive service: %w", err)
	}
	return nil
}

// Restore makes an archived service bookable again; a deleted service stays not found.
func (c *Catalog) Restore(ctx context.Context, id int64) error {
	if err := c.repo.SetStatus(ctx, id, model.ServiceActive); err != nil {
		return fmt.Errorf("restore service: %w", err)
	}
	return nil
}

// Delete soft-deletes a service: it disappears from the API but bookings keep pointing at it.
func (c *Catalog) Delete(ctx context.Context, id int64) error {
	if err := c.repo.SetStatus(ctx, id, model.ServiceDeleted); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}
