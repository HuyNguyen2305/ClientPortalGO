package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"clientportal/internal/model"
)

var allStatuses = []model.Status{
	model.StatusDraft, model.StatusOpen, model.StatusPartiallyPaid, model.StatusPaid,
	model.StatusVoid, model.StatusAccepted, model.StatusDeclined,
}

type edge struct{ from, to model.Status }

func TestCanTransition(t *testing.T) {
	allowed := map[model.ParentType]map[edge]bool{
		model.ParentInvoice: {
			{model.StatusDraft, model.StatusOpen}:                  true,
			{model.StatusDraft, model.StatusVoid}:                  true,
			{model.StatusOpen, model.StatusVoid}:                   true,
			{model.StatusOpen, model.StatusPartiallyPaid}:          true,
			{model.StatusOpen, model.StatusPaid}:                   true,
			{model.StatusPartiallyPaid, model.StatusPartiallyPaid}: true,
			{model.StatusPartiallyPaid, model.StatusPaid}:          true,
		},
		model.ParentEstimate: {
			{model.StatusDraft, model.StatusOpen}:    true,
			{model.StatusOpen, model.StatusAccepted}: true,
			{model.StatusOpen, model.StatusDeclined}: true,
		},
	}
	for pt, edges := range allowed {
		for _, from := range allStatuses {
			for _, to := range allStatuses {
				want := edges[edge{from, to}]
				assert.Equal(t, want, model.CanTransition(pt, from, to), "%s: %s -> %s", pt, from, to)
			}
		}
	}
	assert.False(t, model.CanTransition("quote", model.StatusDraft, model.StatusOpen))
}

func TestValidStatus(t *testing.T) {
	tests := []struct {
		pt   model.ParentType
		s    model.Status
		want bool
	}{
		{model.ParentInvoice, model.StatusAccepted, false},
		{model.ParentEstimate, model.StatusPaid, false},
		{model.ParentInvoice, "bogus", false},
		{model.ParentEstimate, "bogus", false},
		{model.ParentInvoice, model.StatusDraft, true},
		{model.ParentInvoice, model.StatusOpen, true},
		{model.ParentInvoice, model.StatusPaid, true},
		{model.ParentInvoice, model.StatusVoid, true},
		{model.ParentEstimate, model.StatusDraft, true},
		{model.ParentEstimate, model.StatusOpen, true},
		{model.ParentEstimate, model.StatusDeclined, true},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, model.ValidStatus(tt.pt, tt.s), "%s %s", tt.pt, tt.s)
	}
}

func TestSumItems(t *testing.T) {
	tests := []struct {
		name  string
		items []model.Item
		want  int64
	}{
		{"no items", nil, 0},
		{"one item", []model.Item{{Quantity: 1, UnitPriceCents: 6500}}, 6500},
		{"two items", []model.Item{{Quantity: 2, UnitPriceCents: 6500}, {Quantity: 3, UnitPriceCents: 100}}, 13300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, model.SumItems(tt.items))
		})
	}
}

func TestItemTotalCents(t *testing.T) {
	assert.Equal(t, int64(19500), model.Item{Quantity: 3, UnitPriceCents: 6500}.TotalCents())
}

func TestStatusAfterPayment(t *testing.T) {
	assert.Equal(t, model.StatusPartiallyPaid, model.StatusAfterPayment(6500, 1000))
	assert.Equal(t, model.StatusPaid, model.StatusAfterPayment(6500, 6500))
}

func TestBalanceDueCents(t *testing.T) {
	d := model.BillingDocument{TotalCents: 6500, AmountPaidCents: 1000}
	assert.Equal(t, int64(5500), d.BalanceDueCents())
}
