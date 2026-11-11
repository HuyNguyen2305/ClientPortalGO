package handler_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/handler"
	"clientportal/internal/model"
)

func TestPathID(t *testing.T) {
	tests := []struct {
		raw     string
		want    int64
		wantErr bool
	}{
		{"12", 12, false},
		{"0", 0, true},
		{"-3", 0, true},
		{"abc", 0, true},
		{"9999999999999999999999", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Params = gin.Params{{Key: "id", Value: tt.raw}}
			got, err := handler.PathID(c)
			if tt.wantErr {
				assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestQueryInt64(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    *int64
		wantErr bool
	}{
		{"absent", "", nil, false},
		{"positive", "?location_id=7", ptr(int64(7)), false},
		{"zero", "?location_id=0", nil, true},
		{"not a number", "?location_id=x", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/"+tt.query, nil)
			got, err := handler.QueryInt64(c, "location_id")
			if tt.wantErr {
				assert.True(t, errors.Is(err, model.ErrValidation), "got %v", err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func ptr[T any](v T) *T { return &v }
