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
	Currency    string    `json:"currency"`
	Summary     string    `json:"summary"`
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

// NotifyWithin delivers ev but stops waiting for the webhook after wait, so a
// slow endpoint cannot hold up an API response.
func (n *Notifier) NotifyWithin(ev Event, wait time.Duration) error {
	if n.url == "" {
		return nil
	}
	// Not tied to the request context: the request may finish first.
	done := goResult(func() error { return n.Notify(context.Background(), ev) })
	return waitOrTimeout(done, wait)
}

// Notify posts ev to the webhook. It is a no-op when no URL is configured.
func (n *Notifier) Notify(ctx context.Context, ev Event) error {
	if n.url == "" {
		return nil
	}
	ev.Summary = fmt.Sprintf("%s of %.2f %s on account %d", ev.Type, float64(ev.AmountCents)/100, ev.Currency, ev.AccountID)
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
