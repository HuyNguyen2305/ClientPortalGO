// Package config loads runtime settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // BUSINESS_TZ must resolve on hosts without a zoneinfo database (Windows, scratch images)

	"clientportal/internal/model"
)

// Config holds the settings the API needs at startup.
type Config struct {
	DatabaseURL  string
	AdminAPIKey  string
	Port         string
	DBMaxConns   int32
	QueryTimeout time.Duration
	// SessionTTL is how long a login stays valid.
	SessionTTL time.Duration
	// MailAPIKey switches reset emails from the log to the mail provider; MailFrom is the sender address.
	MailAPIKey string
	MailFrom   string
	// ResetURLBase is the front-end page a reset email links to; the token is appended as ?token=.
	ResetURLBase string
	// AppEnv is "production" or anything else; production refuses to start without a mail provider.
	AppEnv string
	// TrustedProxies are the proxy IPs or CIDRs whose X-Forwarded-For header is believed; empty trusts none.
	TrustedProxies []string
	Hours          model.Hours
	// MaxLiveBookings is how many upcoming bookings one customer may hold.
	MaxLiveBookings int
}

// minAdminKeyLen rejects placeholder keys such as "change-me".
const minAdminKeyLen = 32

// Load reads the environment. An empty variable means "use the default".
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		AdminAPIKey:  os.Getenv("ADMIN_API_KEY"),
		Port:         "8080",
		DBMaxConns:   10,
		QueryTimeout: 5 * time.Second,
		SessionTTL:   30 * 24 * time.Hour,
		MailAPIKey:   os.Getenv("MAIL_API_KEY"),
		MailFrom:     os.Getenv("MAIL_FROM"),
		ResetURLBase: envOr("RESET_URL_BASE", "http://localhost:3000/reset-password"),
		AppEnv:       os.Getenv("APP_ENV"),
	}
	if v := os.Getenv("TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies = strings.Split(v, ",")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.AdminAPIKey == "" {
		return Config{}, errors.New("ADMIN_API_KEY is required")
	}
	if len(cfg.AdminAPIKey) < minAdminKeyLen {
		return Config{}, fmt.Errorf("ADMIN_API_KEY must be at least %d characters", minAdminKeyLen)
	}
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}
	if v := os.Getenv("DB_MAX_CONNS"); v != "" {
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("DB_MAX_CONNS must be a positive integer, got %q", v)
		}
		cfg.DBMaxConns = int32(n)
	}
	if v := os.Getenv("QUERY_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("QUERY_TIMEOUT must be a positive duration, got %q", v)
		}
		cfg.QueryTimeout = d
	}
	if cfg.AppEnv == "production" && cfg.MailAPIKey == "" {
		return Config{}, errors.New("MAIL_API_KEY is required when APP_ENV is production")
	}
	if cfg.MailAPIKey != "" && cfg.MailFrom == "" {
		return Config{}, errors.New("MAIL_FROM is required when MAIL_API_KEY is set")
	}
	if v := os.Getenv("SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("SESSION_TTL must be a positive duration, got %q", v)
		}
		cfg.SessionTTL = d
	}
	hours, err := loadHours()
	if err != nil {
		return Config{}, err
	}
	cfg.Hours = hours
	if cfg.MaxLiveBookings, err = positiveInt("BOOKING_MAX_LIVE", "3"); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func clockMinutes(key, def string) (int, error) {
	t, err := time.Parse("15:04", envOr(key, def))
	if err != nil {
		return 0, fmt.Errorf("%s must be HH:MM", key)
	}
	return t.Hour()*60 + t.Minute(), nil
}

func positiveInt(key, def string) (int, error) {
	n, err := strconv.Atoi(envOr(key, def))
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return n, nil
}

// loadHours reads the booking settings: BUSINESS_TZ, BOOKING_OPEN, BOOKING_CLOSE,
// BOOKING_STEP_MINUTES and BOOKING_HORIZON_DAYS.
func loadHours() (model.Hours, error) {
	var h model.Hours
	var err error
	if h.Loc, err = time.LoadLocation(envOr("BUSINESS_TZ", "America/Chicago")); err != nil {
		return h, fmt.Errorf("BUSINESS_TZ: %w", err)
	}
	if h.Open, err = clockMinutes("BOOKING_OPEN", "07:00"); err != nil {
		return h, err
	}
	if h.Close, err = clockMinutes("BOOKING_CLOSE", "17:00"); err != nil {
		return h, err
	}
	if h.Close <= h.Open {
		return h, errors.New("BOOKING_CLOSE must be after BOOKING_OPEN")
	}
	if h.Step, err = positiveInt("BOOKING_STEP_MINUTES", "15"); err != nil {
		return h, err
	}
	// Service durations are multiples of 15, so the grid must be too.
	if h.Step%15 != 0 {
		return h, errors.New("BOOKING_STEP_MINUTES must be a multiple of 15")
	}
	if h.HorizonDays, err = positiveInt("BOOKING_HORIZON_DAYS", "90"); err != nil {
		return h, err
	}
	return h, nil
}
