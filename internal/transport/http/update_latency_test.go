package http

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
)

type updateLatencyTransport struct {
	base  http.RoundTripper
	delay time.Duration
}

func (t updateLatencyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	timer := time.NewTimer(t.delay)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		return nil, r.Context().Err()
	case <-timer.C:
	}
	return t.base.RoundTrip(r)
}

// One complete foreground HTTP read, including both metadata downloads,
// signature validation and the status envelope. The 800 ms per-file latency
// models the observed 0.79–0.92 s metadata reads without an external request.
func BenchmarkFreshUpdateVerification(b *testing.B) {
	s, _, _ := releaseUpdateServer(b, b.TempDir(), "v0.11.0")
	client := s.releaseUpdate.Env.Client
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = updateLatencyTransport{base: transport, delay: 800 * time.Millisecond}
	server := httptest.NewServer(http.HandlerFunc(s.updateRoute))
	defer server.Close()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		response, err := server.Client().Get(server.URL + "/v1/update/fresh")
		if err != nil {
			b.Fatal(err)
		}
		var status contract.UpdateStatus
		err = json.NewDecoder(response.Body).Decode(&status)
		io.Copy(io.Discard, response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || status.State != contract.UpdateStateUpdateAvailable || status.Latest.Version != "v0.11.0" {
			b.Fatalf("fresh verified status: %d %+v %v", response.StatusCode, status, err)
		}
	}
}

type updateFailureTransport struct{ base http.RoundTripper }

func (t updateFailureTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasSuffix(r.URL.Path, "manifest.sig.json") {
		return &http.Response{StatusCode: http.StatusGatewayTimeout, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
	}
	return t.base.RoundTrip(r)
}

// A successful manifest and cached verified release cannot hide the current
// signature download's failure, regardless of the two downloads' ordering.
func TestFreshUpdateReadStillRequiresCurrentSignature(t *testing.T) {
	s, _, _ := releaseUpdateServer(t, t.TempDir(), "v0.11.0")
	if check := s.releaseUpdate.Checker.Refresh(context.Background()); check.Err != nil {
		t.Fatal(check.Err)
	}
	client := s.releaseUpdate.Env.Client
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = updateFailureTransport{base: base}
	server := httptest.NewServer(http.HandlerFunc(s.updateRoute))
	defer server.Close()
	res, err := server.Client().Get(server.URL + "/v1/update/fresh")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var status contract.UpdateStatus
	if err := json.NewDecoder(res.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || status.State != contract.UpdateStateUnknown || status.Latest.Version != "" || !strings.Contains(status.Reason, "504") {
		t.Fatalf("failed current signature was offered: %+v", status)
	}
	if last := s.releaseUpdate.Checker.Last(); last.Latest == nil || last.Latest.Version != "v0.11.0" || last.Err == nil {
		t.Fatalf("failure lost cached evidence or its error: %+v", last)
	}
}
