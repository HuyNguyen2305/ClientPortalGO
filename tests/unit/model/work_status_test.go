package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"clientportal/internal/model"
)

func TestBooking_WorkStatus(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		b    model.Booking
		want model.WorkStatus
	}{
		{"scheduled", model.Booking{Work: model.WorkScheduled}, model.WorkScheduled},
		{"in progress", model.Booking{Work: model.WorkInProgress}, model.WorkInProgress},
		{"completed", model.Booking{Work: model.WorkCompleted}, model.WorkCompleted},
		{"cancelled wins over scheduled", model.Booking{Work: model.WorkScheduled, CancelledAt: &now}, model.WorkCancelled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.b.WorkStatus())
		})
	}
}
