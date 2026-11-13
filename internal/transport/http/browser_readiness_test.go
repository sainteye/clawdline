package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/browserbridge"
	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func TestBrowserSetupSeesAnIsolatedHostBeforePermissionIsGranted(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	poll := httptest.NewRequest(http.MethodPost, "/v1/browser/host/poll", strings.NewReader(`{"mode":"isolated","version":"setup-test"}`)).WithContext(context.WithValue(ctx, accessKey{}, access{machine: true}))
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { s.browserHostPollRoute(response, poll); close(done) }()
	deadline := time.Now().Add(time.Second)
	for {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v1/browser/setup", nil).WithContext(context.WithValue(context.Background(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read)}}))
		s.browserSetupRoute(rec, req)
		var setup struct {
			Connected, Enabled bool
			Mode               string
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &setup); err != nil {
			t.Fatal(err)
		}
		if setup.Enabled || setup.Mode != "native" {
			t.Fatalf("probe granted permission: %s", rec.Body)
		}
		if setup.Connected {
			break
		}
		select {
		case <-done:
			t.Fatalf("setup host was rejected: %d %s", response.Code, response.Body)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("setup never observed transport readiness")
		}
		time.Sleep(time.Millisecond)
	}
	policy, err := s.browserPolicy()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.browsers().DoSession(context.Background(), "c1", "open", json.RawMessage(`{"url":"https://example.com"}`), policy, "", false); err != browserbridge.NativeTools {
		t.Fatalf("readiness changed automatic permission: %v", err)
	}
	cancel()
	<-done
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"commands":[]`) {
		t.Fatalf("probe got commands: %d %s", response.Code, response.Body)
	}
	wrong := httptest.NewRecorder()
	probe, cancelProbe := context.WithCancel(context.Background())
	cancelProbe()
	req := httptest.NewRequest(http.MethodPost, "/v1/browser/host/poll", strings.NewReader(`{"mode":"user_chrome"}`)).WithContext(context.WithValue(probe, accessKey{}, access{machine: true}))
	s.browserHostPollRoute(wrong, req)
	if wrong.Code != http.StatusOK {
		t.Fatalf("a connected usual-Chrome host cannot report readiness independently: %d", wrong.Code)
	}
}
