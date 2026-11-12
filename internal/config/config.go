// Package config loads runtime settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
	_ "time/tzdata" // BUSINESS_TZ must resolve on hosts without a zoneinfo database (Windows, scratch images)

	"clientportal/internal/model"
)

// Config holds the settings the API needs at startup.
type Config struct {
	DatabaseURL  string
	Port         string
	DBMaxConns   int32
	QueryTimeout time.Duration
	Hours        model.Hours
}

// Load reads the environment. An empty variable means "use the default".
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		Port:         "8080",
		DBMaxConns:   10,
		QueryTimeout: 5 * time.Second,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
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
	hours, err := loadHours()
	if err != nil {
		return Config{}, err
	}
	cfg.Hours = hours
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
