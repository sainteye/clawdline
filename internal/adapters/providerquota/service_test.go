package providerquota

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderPayloadsKeepZeroUnknownAndWindowIdentity(t *testing.T) {
	cases := []struct {
		name, id, body string
		count          int
		percent        float64
		window         string
	}{
		{"gemini remaining zero", "gemini", `{"buckets":[{"modelId":"gemini-pro","remainingFraction":0,"resetTime":"2026-12-01T00:00:00Z"}]}`, 1, 100, "gemini-pro"},
		{"gemini unused", "gemini", `{"buckets":[{"modelId":"gemini-flash","remainingFraction":1}]}`, 1, 0, "gemini-flash"},
		{"gemini missing fraction", "gemini", `{"buckets":[{"modelId":"gemini-pro"}]}`, 0, 0, ""},
		{"gemini malformed fraction", "gemini", `{"buckets":[{"modelId":"gemini-pro","remainingFraction":2}]}`, 0, 0, ""},
		{"grok explicit zero", "grok", `{"config":{"creditUsagePercent":0}}`, 1, 0, "7d"},
		{"grok unknown", "grok", `{"config":{"onDemandUsed":{"val":0}}}`, 0, 0, ""},
		{"grok weekly default zero", "grok", `{"currentPeriod":{"type":"USAGE_PERIOD_TYPE_WEEKLY","start":"2026-10-01T00:00:00Z","end":"2026-10-08T00:00:00Z"},"billingPeriodStart":"2026-10-01T00:00:00Z","billingPeriodEnd":"2026-10-08T00:00:00Z"}`, 1, 0, "7d"},
		{"grok monthly never weekly", "grok", `{"config":{"monthlyLimit":{"val":"200"},"used":{"val":"50"}}}`, 1, 25, "monthly"},
		{"grok bad money", "grok", `{"monthlyLimit":{"val":"200oops"},"used":{"val":"50"}}`, 0, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rows []Window
			var err error
			if tc.id == "gemini" {
				rows, err = parseGemini([]byte(tc.body))
			} else {
				rows, err = parseGrok([]byte(tc.body))
			}
			if err != nil || len(rows) != tc.count {
				t.Fatalf("rows=%+v err=%v", rows, err)
			}
			if len(rows) > 0 && (rows[0].UsedPercent != tc.percent || rows[0].Name != tc.window) {
				t.Fatalf("rows=%+v", rows)
			}
		})
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func writeCredential(t *testing.T, home, id, body string) {
	t.Helper()
	dir := filepath.Join(home, "."+id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := "auth.json"
	if id == "gemini" {
		name = "oauth_creds.json"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func waitFinished(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		s.mu.Lock()
		busy := false
		for _, v := range s.slots {
			busy = busy || v.busy
		}
		s.mu.Unlock()
		if !busy {
			return
		}
		select {
		case <-deadline:
			t.Fatal("refresh did not finish")
		case <-time.After(time.Millisecond):
		}
	}
}
func TestMissingCredentialsNeverCallProviders(t *testing.T) {
	s := New(t.TempDir(), nil)
	s.getenv = func(string) string { return "" }
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("provider called without credentials")
		return response(500, ""), nil
	})
	got := s.Snapshot(time.Now())
	if len(got.Providers) != 2 || got.Updating {
		t.Fatalf("%+v", got)
	}
	for _, r := range got.Providers {
		if len(r.Windows) > 0 || r.Problem != "not_signed_in" {
			t.Fatalf("%+v", r)
		}
	}
}
func TestSingleFlightCooldownFailuresAndAccountChanges(t *testing.T) {
	home := t.TempDir()
	writeCredential(t, home, "grok", `{"https://auth.x.ai":{"key":"synthetic-token","user_id":"synthetic-user"}}`)
	s := New(home, nil)
	s.getenv = func(string) string { return "" }
	var hits atomic.Int64
	var status atomic.Int64
	status.Store(200)
	gate := make(chan struct{})
	started := make(chan struct{})
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		if hits.Add(1) == 1 {
			close(started)
			<-gate
		}
		if r.URL.String() != "https://cli-chat-proxy.grok.com/v1/billing?format=credits" || r.Header.Get("X-XAI-Token-Auth") != "xai-grok-cli" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Errorf("unexpected request contract")
		}
		return response(int(status.Load()), `{"config":{"creditUsagePercent":37}}`), nil
	})
	now := time.Now()
	s.Snapshot(now)
	<-started
	for i := 0; i < 20; i++ {
		s.Snapshot(now)
	}
	if hits.Load() != 1 {
		t.Fatalf("concurrent requests=%d", hits.Load())
	}
	close(gate)
	waitFinished(t, s)
	got := s.Snapshot(now)
	if got.Providers[1].Windows[0].UsedPercent != 37 {
		t.Fatalf("%+v", got)
	}
	// HTTP 401 retains a last-good number as stale, without exposing the provider body.
	status.Store(401)
	s.Snapshot(now.Add(RefreshSecondsLimit * time.Second))
	waitFinished(t, s)
	got = s.Snapshot(now.Add(RefreshSecondsLimit * time.Second))
	r := got.Providers[1]
	if !r.Stale || r.Problem != "reauth_required" || len(r.Windows) != 1 {
		t.Fatalf("%+v", r)
	}
	encoded, _ := json.Marshal(got)
	if strings.Contains(string(encoded), "synthetic-token") || strings.Contains(string(encoded), "synthetic-user") {
		t.Fatal("credential or account identifier leaked")
	}
	if hits.Load() != 2 {
		t.Fatalf("unexpected retry count=%d", hits.Load())
	}
	// A different account cannot inherit the old account's percentage.
	writeCredential(t, home, "grok", `{"https://auth.x.ai":{"key":"other-token","expires_at":"2020-01-01T00:00:00Z"}}`)
	got = s.Snapshot(now)
	if len(got.Providers[1].Windows) != 0 || got.Updating {
		t.Fatalf("account switch leaked previous reading: %+v", got)
	}
}
func TestGeminiQueriesOnlyQuotaMetadata(t *testing.T) {
	home := t.TempDir()
	writeCredential(t, home, "gemini", `{"access_token":"synthetic-token","expiry_date":4102444800000}`)
	s := New(home, nil)
	s.getenv = func(string) string { return "" }
	var hits atomic.Int64
	s.client.Transport = transportFunc(func(r *http.Request) (*http.Response, error) {
		hits.Add(1)
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
			t.Error("wrong metadata request")
		}
		if strings.HasSuffix(r.URL.Path, ":loadCodeAssist") {
			return response(200, `{"cloudaicompanionProject":{"id":"synthetic-project"}}`), nil
		}
		if !strings.HasSuffix(r.URL.Path, ":retrieveUserQuota") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"project":"synthetic-project"}` {
			t.Errorf("unexpected body %s", body)
		}
		return response(200, `{"buckets":[{"modelId":"gemini-pro","remainingFraction":0.25}]}`), nil
	})
	s.Snapshot(time.Now())
	waitFinished(t, s)
	got := s.Snapshot(time.Now())
	if hits.Load() != 2 || got.Providers[0].Windows[0].UsedPercent != 75 {
		t.Fatalf("hits=%d snapshot=%+v", hits.Load(), got)
	}
}
func TestRateLimitAndResponseSizeAreBounded(t *testing.T) {
	s := New(t.TempDir(), nil)
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		r := response(429, "")
		r.Header.Set("Retry-After", "3600")
		return r, nil
	})
	_, problem, retry := s.request(context.Background(), "GET", "https://example.invalid", credential{}, "")
	if problem != "rate_limited" || time.Until(retry) < 59*time.Minute {
		t.Fatalf("%s %v", problem, retry)
	}
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return response(200, strings.Repeat("x", ResponseBytesLimit+1)), nil
	})
	_, problem, _ = s.request(context.Background(), "GET", "https://example.invalid", credential{}, "")
	if problem != "invalid_response" {
		t.Fatalf("oversized response: %s", problem)
	}
}
func TestExpiredCredentialsDoNotRefreshOrWriteLogin(t *testing.T) {
	home := t.TempDir()
	writeCredential(t, home, "gemini", `{"access_token":"synthetic","expiry_date":1}`)
	s := New(home, nil)
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("expired credential made an HTTP call")
		return response(500, ""), nil
	})
	before, _ := os.ReadFile(filepath.Join(home, ".gemini/oauth_creds.json"))
	got := s.Snapshot(time.Now())
	after, _ := os.ReadFile(filepath.Join(home, ".gemini/oauth_creds.json"))
	if string(before) != string(after) || got.Providers[0].Problem != "reauth_required" {
		t.Fatalf("%+v", got)
	}
}
