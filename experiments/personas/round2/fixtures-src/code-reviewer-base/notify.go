package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Event is posted to the configured webhook when money moves.
type Event struct {
	Type        string    `json:"type"`
	AccountID   int64     `json:"account_id"`
	AmountCents int64     `json:"amount_cents"`
	Reference   string    `json:"reference,omitempty"`
	At          time.Time `json:"at"`
}

// Notifier delivers events to a webhook endpoint.
type Notifier struct {
	url    string
	client *http.Client
}

func NewNotifier(url string, timeout time.Duration) *Notifier {
	return &Notifier{url: url, client: &http.Client{Timeout: timeout}}
}

// Notify posts ev to the webhook. It is a no-op when no URL is configured.
func (n *Notifier) Notify(ctx context.Context, ev Event) error {
	if n.url == "" {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook: unexpected status %d", resp.StatusCode)
	}
	return nil
}
