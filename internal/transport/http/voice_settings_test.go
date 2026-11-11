package http

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func voiceSettingsCall(t *testing.T, s *Server, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/settings/voice", strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	s.voiceSettingsRoute(rec, req)
	return rec
}

// The route a paired phone reaches: the five voice keys and what the machine
// offers for them, and never another setting.
func TestVoiceSettingsAnswersOnlyTheVoiceCards(t *testing.T) {
	was := voiceProjectNames
	voiceProjectNames = func(*Server) []string { return []string{"orchard-api"} }
	t.Cleanup(func() { voiceProjectNames = was })
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}

	rec := voiceSettingsCall(t, s, http.MethodPost, `{"voice_polish":true,"voice_vocabulary":["Tailscale"],"voice_polish_level":"punctuation"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := []string{"voice_polish", "voice_polish_assistant", "voice_polish_level", "voice_vocabulary",
		"voice_vocabulary_projects", "polish_assistants", "vocabulary_builtin", "vocabulary_projects"}
	if len(got) != len(want) {
		t.Fatalf("answered %d keys: %v", len(got), got)
	}
	for _, key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("no %s in %v", key, got)
		}
	}
	if got["voice_polish"] != true || got["voice_polish_level"] != "punctuation" || got["voice_polish_assistant"] != nil {
		t.Fatalf("values %v", got)
	}
	if list, _ := got["voice_vocabulary"].([]any); len(list) != 1 || list[0] != "Tailscale" {
		t.Fatalf("vocabulary %v", got["voice_vocabulary"])
	}
	if list, _ := got["vocabulary_projects"].([]any); len(list) != 1 || list[0] != "orchard-api" {
		t.Fatalf("projects %v", got["vocabulary_projects"])
	}

	// Read back through the full settings route: the same file.
	read := voiceSettingsCall(t, s, http.MethodGet, "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"voice_polish":true`) {
		t.Fatalf("read: %d %s", read.Code, read.Body)
	}
}

func TestVoiceSettingsRefusesEveryOtherSetting(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	for _, body := range []string{`{"hotkey":"cmd+k"}`, `{"voice_polish":true,"remote":true}`, `{"voice_language":"en"}`} {
		if rec := voiceSettingsCall(t, s, http.MethodPost, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, rec.Code, rec.Body)
		}
	}
	if rec := voiceSettingsCall(t, s, http.MethodPost, `{"voice_vocabulary":["a","A"]}`); rec.Code != http.StatusBadRequest ||
		!strings.Contains(rec.Body.String(), "invalid_voice_vocabulary") {
		t.Errorf("duplicate terms: %d %s", rec.Code, rec.Body)
	}
}

func TestVoiceSettingsWriteNeedsLocalOrSendAuthority(t *testing.T) {
	path := "/v1/settings/voice"
	readOnly := auth.Verdict{Allowed: true, Caps: auth.NewCaps(auth.Read)}
	if status, code, _ := writePolicy(http.MethodPost, path, false, readOnly); status != http.StatusForbidden || code != "forbidden" {
		t.Fatalf("read-capability caller: %d %q", status, code)
	}
	for name, verdict := range map[string]auth.Verdict{
		"paired sender": {Allowed: true, Caps: auth.NewCaps(auth.Read, auth.Send)},
		"local console": {Allowed: true, Local: true, Caps: auth.NewCaps(auth.Read)},
	} {
		if status, code, message := writePolicy(http.MethodPost, path, false, verdict); status != 0 {
			t.Fatalf("%s: %d %q %q", name, status, code, message)
		}
	}
	if status, _, _ := writePolicy(http.MethodGet, path, false, readOnly); status != 0 {
		t.Fatalf("a read was treated as a write: %d", status)
	}
}
