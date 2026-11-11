// Package config loads runtime settings from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the settings the API needs at startup.
type Config struct {
	DatabaseURL  string
	Port         string
	DBMaxConns   int32
	QueryTimeout time.Duration
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
	return cfg, nil
}
