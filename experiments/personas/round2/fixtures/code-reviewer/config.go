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
	NotifyWait     time.Duration // how long a request waits for the webhook

	// DailyTransferLimit is the most one account may send to other accounts
	// per business day, in whole currency units (1000 means 1,000.00).
	// Zero disables the limit.
	DailyTransferLimit int64
	// BusinessLocation decides when a business day starts and ends.
	BusinessLocation *time.Location

	PayoutURL     string
	PayoutAPIKey  string
	PayoutTimeout time.Duration
}

// LoadConfig reads the configuration from the environment.
func LoadConfig() (Config, error) {
	cfg := Config{
		Addr:               envOr("WALLET_ADDR", ":8080"),
		DBDriver:           envOr("WALLET_DB_DRIVER", "sqlite"),
		DSN:                envOr("WALLET_DSN", "file:wallet.db"),
		MaxPageSize:        100,
		WebhookURL:         os.Getenv("WALLET_WEBHOOK_URL"),
		WebhookTimeout:     5 * time.Second,
		NotifyWait:         300 * time.Millisecond,
		DailyTransferLimit: 1000,
		PayoutURL:          envOr("WALLET_PAYOUT_URL", "https://payouts.example.invalid"),
		PayoutAPIKey:       os.Getenv("WALLET_PAYOUT_API_KEY"),
		PayoutTimeout:      10 * time.Second,
	}

	if v := os.Getenv("WALLET_MAX_PAGE_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("WALLET_MAX_PAGE_SIZE: want a positive integer, got %q", v)
		}
		cfg.MaxPageSize = n
	}
	if v := os.Getenv("WALLET_DAILY_TRANSFER_LIMIT"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("WALLET_DAILY_TRANSFER_LIMIT: want a non-negative integer, got %q", v)
		}
		cfg.DailyTransferLimit = n
	}
	for key, dst := range map[string]*time.Duration{
		"WALLET_WEBHOOK_TIMEOUT": &cfg.WebhookTimeout,
		"WALLET_NOTIFY_WAIT":     &cfg.NotifyWait,
		"WALLET_PAYOUT_TIMEOUT":  &cfg.PayoutTimeout,
	} {
		if v := os.Getenv(key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return Config{}, fmt.Errorf("%s: %w", key, err)
			}
			*dst = d
		}
	}

	loc, err := time.LoadLocation(envOr("WALLET_BUSINESS_TZ", "America/New_York"))
	if err != nil {
		return Config{}, fmt.Errorf("WALLET_BUSINESS_TZ: %w", err)
	}
	cfg.BusinessLocation = loc
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// startOfBusinessDay returns midnight at the start of the business day that
// contains now.
func startOfBusinessDay(now time.Time, loc *time.Location) time.Time {
	y, m, d := now.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}
