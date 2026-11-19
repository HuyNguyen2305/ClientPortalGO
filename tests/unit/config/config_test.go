package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"clientportal/internal/config"
	"clientportal/internal/model"
)

const adminKey = "0123456789abcdef0123456789abcdef" // the minimum length

func TestLoad(t *testing.T) {
	keys := []string{"DATABASE_URL", "ADMIN_API_KEY", "PORT", "DB_MAX_CONNS", "QUERY_TIMEOUT", "BOOKING_MAX_LIVE", "SESSION_TTL", "MAIL_API_KEY", "MAIL_FROM", "RESET_URL_BASE", "APP_ENV", "TRUSTED_PROXIES"}
	tests := []struct {
		name    string
		env     map[string]string
		want    config.Config
		wantErr bool
	}{
		{"defaults", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey},
			config.Config{DatabaseURL: "postgres://x", AdminAPIKey: adminKey, Port: "8080", DBMaxConns: 10, QueryTimeout: 5 * time.Second, SessionTTL: 720 * time.Hour, ResetURLBase: "http://localhost:3000/reset-password", MaxLiveBookings: 3}, false},
		{"overrides", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "PORT": "9000", "DB_MAX_CONNS": "25", "QUERY_TIMEOUT": "2s", "BOOKING_MAX_LIVE": "5", "SESSION_TTL": "1h"},
			config.Config{DatabaseURL: "postgres://x", AdminAPIKey: adminKey, Port: "9000", DBMaxConns: 25, QueryTimeout: 2 * time.Second, SessionTTL: time.Hour, ResetURLBase: "http://localhost:3000/reset-password", MaxLiveBookings: 5}, false},
		{"mail settings", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "MAIL_API_KEY": "re_1", "MAIL_FROM": "a@b.c", "RESET_URL_BASE": "https://p.test/reset"},
			config.Config{DatabaseURL: "postgres://x", AdminAPIKey: adminKey, Port: "8080", DBMaxConns: 10, QueryTimeout: 5 * time.Second, SessionTTL: 720 * time.Hour, MailAPIKey: "re_1", MailFrom: "a@b.c", ResetURLBase: "https://p.test/reset", MaxLiveBookings: 3}, false},
		{"short admin key", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": "change-me"}, config.Config{}, true},
		{"production without mail key", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "APP_ENV": "production"}, config.Config{}, true},
		{"production with mail key", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "APP_ENV": "production", "MAIL_API_KEY": "re_1", "MAIL_FROM": "a@b.c"},
			config.Config{DatabaseURL: "postgres://x", AdminAPIKey: adminKey, AppEnv: "production", Port: "8080", DBMaxConns: 10, QueryTimeout: 5 * time.Second, SessionTTL: 720 * time.Hour, MailAPIKey: "re_1", MailFrom: "a@b.c", ResetURLBase: "http://localhost:3000/reset-password", MaxLiveBookings: 3}, false},
		{"trusted proxies", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "TRUSTED_PROXIES": "10.0.0.1,10.0.1.0/24"},
			config.Config{DatabaseURL: "postgres://x", AdminAPIKey: adminKey, TrustedProxies: []string{"10.0.0.1", "10.0.1.0/24"}, Port: "8080", DBMaxConns: 10, QueryTimeout: 5 * time.Second, SessionTTL: 720 * time.Hour, ResetURLBase: "http://localhost:3000/reset-password", MaxLiveBookings: 3}, false},
		{"mail key without sender", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "MAIL_API_KEY": "re_1"}, config.Config{}, true},
		{"bad session ttl", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "SESSION_TTL": "forever"}, config.Config{}, true},
		{"zero session ttl", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "SESSION_TTL": "0s"}, config.Config{}, true},
		{"zero booking cap", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "BOOKING_MAX_LIVE": "0"}, config.Config{}, true},
		{"missing database url", map[string]string{"ADMIN_API_KEY": adminKey}, config.Config{}, true},
		{"missing admin key", map[string]string{"DATABASE_URL": "postgres://x"}, config.Config{}, true},
		{"bad max conns", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "DB_MAX_CONNS": "many"}, config.Config{}, true},
		{"bad timeout", map[string]string{"DATABASE_URL": "postgres://x", "ADMIN_API_KEY": adminKey, "QUERY_TIMEOUT": "soon"}, config.Config{}, true},
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
			got.Hours = model.Hours{} // covered by TestLoadBookingHours
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadBookingHours(t *testing.T) {
	keys := []string{"BUSINESS_TZ", "BOOKING_OPEN", "BOOKING_CLOSE", "BOOKING_STEP_MINUTES", "BOOKING_HORIZON_DAYS"}
	set := func(t *testing.T, env map[string]string) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("ADMIN_API_KEY", adminKey)
		for _, k := range keys {
			t.Setenv(k, env[k])
		}
	}

	t.Run("defaults", func(t *testing.T) {
		set(t, nil)
		cfg, err := config.Load()
		require.NoError(t, err)
		h := cfg.Hours
		assert.Equal(t, "America/Chicago", h.Loc.String())
		assert.Equal(t, 7*60, h.Open)
		assert.Equal(t, 17*60, h.Close)
		assert.Equal(t, 15, h.Step)
		assert.Equal(t, 90, h.HorizonDays)
	})

	t.Run("overrides", func(t *testing.T) {
		set(t, map[string]string{"BUSINESS_TZ": "Asia/Ho_Chi_Minh", "BOOKING_OPEN": "08:30", "BOOKING_CLOSE": "18:00",
			"BOOKING_STEP_MINUTES": "30", "BOOKING_HORIZON_DAYS": "30"})
		cfg, err := config.Load()
		require.NoError(t, err)
		assert.Equal(t, "Asia/Ho_Chi_Minh", cfg.Hours.Loc.String())
		assert.Equal(t, 8*60+30, cfg.Hours.Open)
		assert.Equal(t, 18*60, cfg.Hours.Close)
		assert.Equal(t, 30, cfg.Hours.Step)
		assert.Equal(t, 30, cfg.Hours.HorizonDays)
	})

	for name, env := range map[string]map[string]string{
		"unknown timezone":    {"BUSINESS_TZ": "Mars/Base"},
		"bad open":            {"BOOKING_OPEN": "7am"},
		"close before open":   {"BOOKING_OPEN": "17:00", "BOOKING_CLOSE": "07:00"},
		"zero step":           {"BOOKING_STEP_MINUTES": "0"},
		"step not 15-aligned": {"BOOKING_STEP_MINUTES": "20"},
		"zero horizon":        {"BOOKING_HORIZON_DAYS": "0"},
	} {
		t.Run(name, func(t *testing.T) {
			set(t, env)
			_, err := config.Load()
			assert.Error(t, err)
		})
	}
}
