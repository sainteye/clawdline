package main

import (
	"testing"
	"time"
)

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		minor    int64
		currency string
		want     string
	}{
		{5, "USD", "0.05"},
		{12345, "EUR", "123.45"},
		{-550, "USD", "-5.50"},
		{1250, "KWD", "1.250"},
		{500, "JPY", "500"},
	}
	for _, tt := range tests {
		if got := FormatAmount(tt.minor, tt.currency); got != tt.want {
			t.Errorf("FormatAmount(%d, %q) = %q, want %q", tt.minor, tt.currency, got, tt.want)
		}
	}
}

func TestStartOfBusinessDay(t *testing.T) {
	tests := []struct {
		zone      string
		hour, min int
		day       int
	}{
		{"UTC", 23, 59, 14},
		{"America/New_York", 9, 30, 14},
		{"America/New_York", 12, 0, 8}, // DST starts on 2026-03-08
		{"Europe/Berlin", 18, 0, 14},
		{"Asia/Tokyo", 17, 45, 14},
	}
	for _, tt := range tests {
		loc, err := time.LoadLocation(tt.zone)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 3, tt.day, tt.hour, tt.min, 0, 0, loc)
		want := time.Date(2026, 3, tt.day, 0, 0, 0, 0, loc)
		if got := startOfBusinessDay(now, loc); !got.Equal(want) {
			t.Errorf("%s: startOfBusinessDay(%v) = %v, want %v", tt.zone, now, got, want)
		}
	}
}
