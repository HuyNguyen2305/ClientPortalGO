package dto

import "clientportal/internal/model"

// CardResponse is a saved card as returned by the API.
type CardResponse struct {
	ID       string `json:"id"`
	Brand    string `json:"brand"`
	Last4    string `json:"last4"`
	ExpMonth int    `json:"exp_month"`
	ExpYear  int    `json:"exp_year"`
}

// FromCard maps a domain card to its response.
func FromCard(c model.Card) CardResponse {
	return CardResponse{ID: c.ID, Brand: c.Brand, Last4: c.Last4, ExpMonth: c.ExpMonth, ExpYear: c.ExpYear}
}

// SetupIntentResponse carries the secret Stripe.js needs to save a card.
type SetupIntentResponse struct {
	ClientSecret string `json:"client_secret"`
}
