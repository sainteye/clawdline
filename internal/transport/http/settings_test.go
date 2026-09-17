package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
)

func settingsCall(t *testing.T, s *Server, method, contentType, body string) (*httptest.ResponseRecorder, contract.SettingsSnapshot, contract.Refusal) {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/settings", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	s.settingsRoute(rec, req)
	var snap contract.SettingsSnapshot
	var refusal contract.Refusal
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &snap); err != nil {
			t.Fatalf("%s: %v in %s", method, err, rec.Body)
		}
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &refusal)
	}
	return rec, snap, refusal
}

// The route reads the file as it is, writes only what it was sent, and refuses
// the write a page elsewhere could make without asking.
func TestSettingsRoute(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "clawdline-next")
	s := &Server{cfg: config.Config{Dir: dir}}

	rec, snap, _ := settingsCall(t, s, http.MethodGet, "", "")
	if rec.Code != http.StatusOK || snap.Exists || snap.Hotkey != nil || snap.ScopeApp != nil {
		t.Fatalf("first read: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"hotkey":null`) {
		t.Fatalf("an absent key is null on the wire: %s", rec.Body)
	}

	// A simple cross-site request: plain text, no preflight.
	rec, _, refusal := settingsCall(t, s, http.MethodPost, "text/plain", `{"hotkey":"cmd+shift+k"}`)
	if rec.Code != http.StatusUnsupportedMediaType || refusal.Error != "unsupported_media_type" {
		t.Fatalf("plain text: %d %s", rec.Code, rec.Body)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("a refused write left a file: %v", err)
	}

	for body, code := range map[string]string{
		`{"hotkey":"k"}`:               "invalid_hotkey",
		`{"scope_app":"com.x;rm -rf"}`: "invalid_scope",
		`{"language":"en"}`:            "bad_request",
		`not json`:                     "bad_request",
	} {
		rec, _, refusal = settingsCall(t, s, http.MethodPost, "application/json", body)
		if rec.Code != http.StatusBadRequest || refusal.Error != code {
			t.Fatalf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}

	rec, snap, _ = settingsCall(t, s, http.MethodPost, "application/json; charset=utf-8",
		`{"hotkey":"cmd+shift+k","scope_app":null}`)
	if rec.Code != http.StatusOK || !snap.Exists || snap.Hotkey == nil || *snap.Hotkey != "cmd+shift+k" || snap.ScopeApp != nil {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	rec, snap, _ = settingsCall(t, s, http.MethodPost, "application/json", `{"scope_app":""}`)
	if rec.Code != http.StatusOK || snap.Hotkey == nil || *snap.Hotkey != "cmd+shift+k" || snap.ScopeApp == nil || *snap.ScopeApp != "" {
		t.Fatalf("second write: %d %s", rec.Code, rec.Body)
	}

	// A file somebody broke by hand is refused and left as it was.
	broken := []byte(`{"hotkey": "cmd+shift+k",`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), broken, 0o600); err != nil {
		t.Fatal(err)
	}
	rec, _, refusal = settingsCall(t, s, http.MethodPost, "application/json", `{"hotkey":"cmd+j"}`)
	if rec.Code != http.StatusConflict || refusal.Error != "settings_file_invalid" {
		t.Fatalf("broken file: %d %s", rec.Code, rec.Body)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, "config.json")); string(after) != string(broken) {
		t.Fatalf("broken file was replaced with %s", after)
	}

	rec, _, _ = settingsCall(t, s, http.MethodDelete, "", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("delete: %d", rec.Code)
	}
}
