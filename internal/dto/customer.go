package dto

import "clientportal/internal/model"

// CustomerRequest is the body for creating or updating a customer.
type CustomerRequest struct {
	Name  string  `json:"name" binding:"required,max=200"`
	Email *string `json:"email" binding:"omitempty,email,max=320"`
	Phone *string `json:"phone" binding:"omitempty,max=50"`
}

// CustomerResponse is a customer as returned by the API.
type CustomerResponse struct {
	ID            int64   `json:"id"`
	AccountNumber int64   `json:"account_number"`
	Name          string  `json:"name"`
	Email         *string `json:"email"`
	Phone         *string `json:"phone"`
}

// FromCustomer maps a domain customer to its response.
func FromCustomer(c model.Customer) CustomerResponse {
	return CustomerResponse{ID: c.ID, AccountNumber: c.AccountNumber, Name: c.Name, Email: c.Email, Phone: c.Phone}
}
