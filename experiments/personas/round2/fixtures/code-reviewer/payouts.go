package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

// PayoutClient sends money from a wallet to an external bank account through
// the payout provider's HTTP API.
type PayoutClient struct {
	baseURL     string
	apiKey      string
	http        *http.Client
	maxAttempts int
	backoff     time.Duration
}

// PayoutRequest is what the wallet asks the provider to pay out.
type PayoutRequest struct {
	AmountCents int64
	Currency    string
	Destination string // provider token for the bank account
	Reference   string // also sent as the idempotency key
}

// PayoutResult is the provider's answer to an accepted payout.
type PayoutResult struct {
	ProviderID string `json:"id"`
	Status     string `json:"status"`
}

// providerPayout is the provider's wire format.
type providerPayout struct {
	AmountMinor int32  `json:"amount_minor"`
	Currency    string `json:"currency"`
	Destination string `json:"destination"`
	Reference   string `json:"reference"`
}

// rejectedError is a 4xx answer from the provider. Retrying cannot help.
type rejectedError struct {
	status int
	body   string
}

func (e *rejectedError) Error() string {
	return fmt.Sprintf("payout rejected: status %d: %s", e.status, e.body)
}

// Send asks the provider to pay out p. Network errors, 429 and 5xx answers
// are retried with exponential backoff (or the provider's Retry-After); the
// provider deduplicates retries by the idempotency key.
func (c *PayoutClient) Send(ctx context.Context, p PayoutRequest) (PayoutResult, error) {
	body, err := json.Marshal(providerPayout{
		AmountMinor: int32(p.AmountCents),
		Currency:    p.Currency,
		Destination: p.Destination,
		Reference:   p.Reference,
	})
	if err != nil {
		return PayoutResult{}, err
	}

	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		resp, err := c.post(ctx, body, p.Reference)
		if err == nil && !retryableStatus(resp.StatusCode) {
			return readPayout(resp)
		}
		if err == nil {
			err = fmt.Errorf("payout provider: status %d", resp.StatusCode)
		}
		log.Printf("payout %s: attempt %d/%d: %v", p.Reference, attempt, c.maxAttempts, err)

		if attempt < c.maxAttempts {
			select {
			case <-time.After(retryDelay(resp, c.backoff<<(attempt-1))):
			case <-ctx.Done():
				return PayoutResult{}, ctx.Err()
			}
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	return PayoutResult{}, err
}

func (c *PayoutClient) post(ctx context.Context, body []byte, idempotencyKey string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/payouts", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Idempotency-Key", idempotencyKey)
	return c.http.Do(req)
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// retryDelay honours a Retry-After header given in seconds, else returns def.
func retryDelay(resp *http.Response, def time.Duration) time.Duration {
	if resp == nil {
		return def
	}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && secs > 0 && secs <= 30 {
		return time.Duration(secs) * time.Second
	}
	return def
}

// readPayout decodes an accepted payout or returns a rejectedError.
func readPayout(resp *http.Response) (PayoutResult, error) {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return PayoutResult{}, &rejectedError{status: resp.StatusCode, body: string(msg)}
	}
	var out PayoutResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return PayoutResult{}, fmt.Errorf("decode payout response: %w", err)
	}
	return out, nil
}
