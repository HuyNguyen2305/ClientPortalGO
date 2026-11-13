// Package service holds the business rules. It depends on repository interfaces, never on Gin.
package service

import (
	"context"
	"fmt"
	"strings"

	"clientportal/internal/model"
)

// CustomerRepository is the storage the customer service needs.
type CustomerRepository interface {
	Create(ctx context.Context, c model.Customer) (model.Customer, error)
	Get(ctx context.Context, id int64) (model.Customer, error)
	List(ctx context.Context, afterID int64, limit int) ([]model.Customer, error)
	Update(ctx context.Context, c model.Customer) (model.Customer, error)
	Delete(ctx context.Context, id int64) error
}

// CustomerService manages customers.
type CustomerService struct {
	repo CustomerRepository
}

// NewCustomerService returns a customer service.
func NewCustomerService(repo CustomerRepository) *CustomerService {
	return &CustomerService{repo: repo}
}

// required trims s and fails with ErrValidation when nothing is left.
func required(field, s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("%w: %s is required", model.ErrValidation, field)
	}
	return s, nil
}

// Create adds a customer.
func (s *CustomerService) Create(ctx context.Context, name string, email, phone *string) (model.Customer, error) {
	name, err := required("name", name)
	if err != nil {
		return model.Customer{}, err
	}
	c, err := s.repo.Create(ctx, model.Customer{Name: name, Email: email, Phone: phone})
	if err != nil {
		return model.Customer{}, fmt.Errorf("create customer: %w", err)
	}
	return c, nil
}

// Get returns one customer.
func (s *CustomerService) Get(ctx context.Context, id int64) (model.Customer, error) {
	c, err := s.repo.Get(ctx, id)
	if err != nil {
		return model.Customer{}, fmt.Errorf("get customer: %w", err)
	}
	return c, nil
}

// List returns one page of customers. nextAfterID is 0 on the last page.
func (s *CustomerService) List(ctx context.Context, afterID int64, limit int) (items []model.Customer, nextAfterID int64, err error) {
	if limit < 1 {
		return nil, 0, fmt.Errorf("%w: limit must be at least 1", model.ErrValidation)
	}
	items, err = s.repo.List(ctx, afterID, limit+1)
	if err != nil {
		return nil, 0, fmt.Errorf("list customers: %w", err)
	}
	if len(items) > limit {
		items = items[:limit]
		nextAfterID = items[limit-1].ID
	}
	return items, nextAfterID, nil
}

// Update replaces a customer's details.
func (s *CustomerService) Update(ctx context.Context, id int64, name string, email, phone *string) (model.Customer, error) {
	name, err := required("name", name)
	if err != nil {
		return model.Customer{}, err
	}
	c, err := s.repo.Update(ctx, model.Customer{ID: id, Name: name, Email: email, Phone: phone})
	if err != nil {
		return model.Customer{}, fmt.Errorf("update customer: %w", err)
	}
	return c, nil
}

// Delete removes a customer that has no locations or documents.
func (s *CustomerService) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete customer: %w", err)
	}
	return nil
}
