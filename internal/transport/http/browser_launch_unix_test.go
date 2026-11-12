//go:build darwin || linux

package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func browserDevice(r *http.Request, caps ...auth.Capability) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), accessKey{}, access{verdict: auth.Verdict{Allowed: true, Caps: auth.NewCaps(caps...)}}))
}

// The setup guide's two routes, with a shell script standing in for Chromium
// so no browser starts.
func TestTheSetupGuideFindsAndStartsClawdlinesChromium(t *testing.T) {
	dir := t.TempDir()
	s := &Server{cfg: config.Config{Dir: dir, Port: 7792}}
	ran := filepath.Join(t.TempDir(), "ran")
	fake := filepath.Join(t.TempDir(), "chromium")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\ntouch '"+ran+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	chromium := ""
	was := findChromium
	findChromium = func() string { return chromium }
	t.Cleanup(func() { findChromium = was })

	setup := func() map[string]any {
		rec := httptest.NewRecorder()
		s.browserSetupRoute(rec, browserDevice(httptest.NewRequest(http.MethodGet, "/v1/browser/setup", nil), auth.Read))
		if rec.Code != http.StatusOK {
			t.Fatalf("setup: %d %s", rec.Code, rec.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}
	launch := func(r *http.Request) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.browserLaunchRoute(rec, r)
		return rec
	}

	got := setup()
	if got["chromium_found"] != false || got["connected"] != false || got["install_command"] != "npx playwright install chromium" || got["profile"] != filepath.Join(dir, "chrome-profile") {
		t.Fatalf("no Chromium: %v", got)
	}
	rec := launch(browserDevice(httptest.NewRequest(http.MethodPost, "/v1/browser/launch", strings.NewReader("{}")), auth.Read, auth.Send))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"browser_chromium_missing"`) || !strings.Contains(rec.Body.String(), "npx playwright install chromium") {
		t.Fatalf("missing Chromium: %d %s", rec.Code, rec.Body)
	}

	chromium = fake
	if got := setup(); got["chromium_found"] != true || got["chromium"] != fake {
		t.Fatalf("found: %v", got)
	}
	for _, refused := range []*http.Request{
		browserDevice(httptest.NewRequest(http.MethodPost, "/v1/browser/launch", nil), auth.Read),
		httptest.NewRequest(http.MethodPost, "/v1/browser/launch", nil).WithContext(context.WithValue(context.Background(), accessKey{}, access{machine: true})),
	} {
		if rec := launch(refused); rec.Code != http.StatusForbidden {
			t.Fatalf("a reader or the machine started Chromium: %d %s", rec.Code, rec.Body)
		}
	}
	if rec := launch(browserDevice(httptest.NewRequest(http.MethodPost, "/v1/browser/launch", strings.NewReader(`{"browser":"/bin/sh"}`)), auth.Send)); rec.Code != http.StatusBadRequest {
		t.Fatalf("a body naming a binary was accepted: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("a refused launch ran Chromium")
	}
	rec = launch(browserDevice(httptest.NewRequest(http.MethodPost, "/v1/browser/launch", nil), auth.Send))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pid"`) {
		t.Fatalf("launch: %d %s", rec.Code, rec.Body)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(ran); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in Chromium never ran")
		}
	}
	launcher, _ := os.ReadFile(filepath.Join(dir, "browser-host.sh"))
	if !strings.Contains(string(launcher), "CLAWDLINE_NEXT_PORT=7792") {
		t.Fatalf("launcher %q", launcher)
	}
	if rec := httptest.NewRecorder(); true {
		s.browserSetupRoute(rec, httptest.NewRequest(http.MethodGet, "/v1/browser/setup", nil))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("an unauthenticated read: %d", rec.Code)
		}
	}
}
