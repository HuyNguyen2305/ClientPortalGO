package handler_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBookingHandler_RejectsBadInput(t *testing.T) {
	tests := []struct{ name, method, path, body string }{
		{"empty body", http.MethodPost, "/api/v1/bookings", `{}`},
		{"service missing", http.MethodPost, "/api/v1/bookings", `{"location_id":7,"date":"2026-11-12","time":"07:00"}`},
		{"location missing", http.MethodPost, "/api/v1/bookings", `{"service_id":2,"date":"2026-11-12","time":"07:00"}`},
		{"date missing", http.MethodPost, "/api/v1/bookings", `{"service_id":2,"location_id":7,"time":"07:00"}`},
		{"time missing", http.MethodPost, "/api/v1/bookings", `{"service_id":2,"location_id":7,"date":"2026-11-12"}`},
		{"series empty body", http.MethodPost, "/api/v1/booking-series", `{}`},
		{"series date missing", http.MethodPost, "/api/v1/booking-series", `{"service_id":2,"location_id":7,"time":"07:00"}`},
		{"bad booking id", http.MethodGet, "/api/v1/bookings/abc", ""},
		{"bad cancel id", http.MethodPost, "/api/v1/bookings/0/cancel", ""},
		{"bad start id", http.MethodPost, "/api/v1/bookings/abc/start", ""},
		{"bad complete id", http.MethodPost, "/api/v1/bookings/0/complete", `{}`},
		{"complete without a body", http.MethodPost, "/api/v1/bookings/1/complete", ""},
		{"work orders bad limit", http.MethodGet, "/api/v1/work-orders?limit=0", ""},
		{"work orders bad cursor", http.MethodGet, "/api/v1/work-orders?cursor=abc", ""},
		{"work orders bad service id", http.MethodGet, "/api/v1/work-orders?service_id=-1", ""},
		{"work orders bad status", http.MethodGet, "/api/v1/work-orders?status=lost", ""},
		{"bad days from", http.MethodGet, "/api/v1/booking/days?from=soon", ""},
		{"bad slots service id", http.MethodGet, "/api/v1/services/abc/slots?date=2026-11-12", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := sendAs(docRouter(t), tt.method, tt.path, tt.body)
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		})
	}
}
