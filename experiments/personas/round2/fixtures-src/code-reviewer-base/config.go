package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the service settings. Every field can be set from the
// environment; see LoadConfig for the variable names and defaults.
type Config struct {
	Addr     string
	DBDriver string
	DSN      string

	// MaxPageSize caps the number of ledger entries returned per page.
	MaxPageSize int

	// WebhookURL receives account events. Empty disables notifications.
	WebhookURL     string
	WebhookTimeout time.Duration
}

// LoadConfig reads the configuration from the environment.
func LoadConfig() (Config, error) {
	cfg := Config{
		Addr:           envOr("WALLET_ADDR", ":8080"),
		DBDriver:       envOr("WALLET_DB_DRIVER", "sqlite"),
		DSN:            envOr("WALLET_DSN", "file:wallet.db"),
		MaxPageSize:    100,
		WebhookURL:     os.Getenv("WALLET_WEBHOOK_URL"),
		WebhookTimeout: 5 * time.Second,
	}

	if v := os.Getenv("WALLET_MAX_PAGE_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("WALLET_MAX_PAGE_SIZE: want a positive integer, got %q", v)
		}
		cfg.MaxPageSize = n
	}
	if v := os.Getenv("WALLET_WEBHOOK_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Config{}, fmt.Errorf("WALLET_WEBHOOK_TIMEOUT: %w", err)
		}
		cfg.WebhookTimeout = d
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
