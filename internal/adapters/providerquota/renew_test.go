package providerquota

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

const (
	expiredRenewable = `{"https://auth.x.ai::client":{"key":"old-token","user_id":"synthetic-user","expires_at":"2020-01-01T00:00:00Z","refresh_token":"synthetic-refresh"}}`
	freshRenewed     = `{"https://auth.x.ai::client":{"key":"new-token","user_id":"synthetic-user","expires_at":"2100-01-01T00:00:00Z","refresh_token":"synthetic-refresh"}}`
	validRenewable   = `{"https://auth.x.ai::client":{"key":"old-token","user_id":"synthetic-user","expires_at":"2100-01-01T00:00:00Z","refresh_token":"synthetic-refresh"}}`
)

type renewHarness struct {
	s      *Service
	renews atomic.Int64
	hits   atomic.Int64
}

// newRenewHarness answers billing with 37% for new-token and 401 for anything else.
func newRenewHarness(t *testing.T, credential string, renew func(home string) error) *renewHarness {
	t.Helper()
	home := t.TempDir()
	writeCredential(t, home, "grok", credential)
	h := &renewHarness{s: New(home, nil)}
	h.s.getenv = func(string) string { return "" }
	h.s.WithGrokRenewal(func(context.Context) error {
		h.renews.Add(1)
		return renew(home)
	})
	h.s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		h.hits.Add(1)
		if r.Header.Get("Authorization") != "Bearer new-token" {
			return response(401, ""), nil
		}
		return response(200, `{"config":{"creditUsagePercent":37}}`), nil
	})
	return h
}

func (h *renewHarness) grok(t *testing.T, now time.Time) Row {
	t.Helper()
	h.s.Grok(now)
	waitFinished(t, h.s)
	return h.s.Grok(now)
}

func TestAnExpiredGrokTokenIsRenewedInsteadOfReportedExpired(t *testing.T) {

	h := newRenewHarness(t, expiredRenewable, func(home string) error {
		writeCredential(t, home, "grok", freshRenewed)
		return nil
	})
	now := time.Now()
	first := h.s.Grok(now)
	if first.Problem == "reauth_required" {
		t.Fatalf("expired-but-renewable token was shown as expired before renewal: %+v", first)
	}
	waitFinished(t, h.s)
	got := h.s.Grok(now)
	if got.Problem != "" || len(got.Windows) != 1 || got.Windows[0].UsedPercent != 37 {
		t.Fatalf("renewed reading: %+v", got)
	}
	if h.renews.Load() != 1 || h.hits.Load() != 1 {
		t.Fatalf("renews=%d hits=%d", h.renews.Load(), h.hits.Load())
	}
	// The renewed file is the same account: the next read keeps the reading.
	for i := 0; i < 5; i++ {
		if again := h.s.Grok(now); len(again.Windows) != 1 {
			t.Fatalf("renewal reset the reading: %+v", again)
		}
	}
	if h.renews.Load() != 1 {
		t.Fatalf("renewal repeated within the cooldown: %d", h.renews.Load())
	}
}

func TestOnlyAFailedRenewalIsReportedExpired(t *testing.T) {
	h := newRenewHarness(t, expiredRenewable, func(string) error { return nil })
	now := time.Now()
	if got := h.grok(t, now); got.Problem != "reauth_required" {
		t.Fatalf("unrenewable token: %+v", got)
	}
	if h.renews.Load() != 1 || h.hits.Load() != 0 {
		t.Fatalf("renews=%d hits=%d", h.renews.Load(), h.hits.Load())
	}
	for i := 0; i < 5; i++ {
		h.s.Grok(now)
	}
	waitFinished(t, h.s)
	if h.renews.Load() != 1 {
		t.Fatalf("failed renewal retried within the cooldown: %d", h.renews.Load())
	}
}

func TestATokenWithoutRefreshTokenIsExpiredWithoutRenewal(t *testing.T) {
	h := newRenewHarness(t, `{"https://auth.x.ai":{"key":"old-token","expires_at":"2020-01-01T00:00:00Z"}}`,
		func(string) error { return nil })
	if got := h.grok(t, time.Now()); got.Problem != "reauth_required" || h.renews.Load() != 0 {
		t.Fatalf("renews=%d row=%+v", h.renews.Load(), got)
	}
}

func TestARejectedGrokTokenIsRenewedAndRetriedOnce(t *testing.T) {
	h := newRenewHarness(t, validRenewable, func(home string) error {
		writeCredential(t, home, "grok", freshRenewed)
		return nil
	})
	got := h.grok(t, time.Now())
	if got.Problem != "" || len(got.Windows) != 1 || h.renews.Load() != 1 || h.hits.Load() != 2 {
		t.Fatalf("renews=%d hits=%d row=%+v", h.renews.Load(), h.hits.Load(), got)
	}
}

func TestAnUnaskableGrokIsUnknownRatherThanExpired(t *testing.T) {
	h := newRenewHarness(t, expiredRenewable, func(string) error { return ErrRenewalUnavailable })
	if got := h.grok(t, time.Now()); got.Problem != "unavailable" {
		t.Fatalf("busy CLI was reported as: %+v", got)
	}
}
