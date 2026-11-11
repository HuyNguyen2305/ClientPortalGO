package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/config"
)

func TestLoad(t *testing.T) {
	keys := []string{"DATABASE_URL", "PORT", "DB_MAX_CONNS", "QUERY_TIMEOUT"}
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr bool
	}{
		{"defaults", map[string]string{"DATABASE_URL": "postgres://x"},
			config.Config{DatabaseURL: "postgres://x", Port: "8080", DBMaxConns: 10, QueryTimeout: 5 * time.Second}, false},
		{"overrides", map[string]string{"DATABASE_URL": "postgres://x", "PORT": "9000", "DB_MAX_CONNS": "25", "QUERY_TIMEOUT": "2s"},
			config.Config{DatabaseURL: "postgres://x", Port: "9000", DBMaxConns: 25, QueryTimeout: 2 * time.Second}, false},
		{"missing database url", map[string]string{}, config.Config{}, true},
		{"bad max conns", map[string]string{"DATABASE_URL": "postgres://x", "DB_MAX_CONNS": "many"}, config.Config{}, true},
		{"bad timeout", map[string]string{"DATABASE_URL": "postgres://x", "QUERY_TIMEOUT": "soon"}, config.Config{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, k := range keys {
				t.Setenv(k, tt.env[k])
			}
			got, err := config.Load()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
