package model

// Card is a saved payment card. Stripe holds the card; this is its display summary.
type Card struct {
	ID       string
	Brand    string
	Last4    string
	ExpMonth int
	ExpYear  int
}
