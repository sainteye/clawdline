package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

// testCopilotKey is assembled so no line of this file reads as a real credential.
var testCopilotKey = "sk-" + "proj-test0000000000000000LAST"

func copilotCall(t *testing.T, s *Server, handler http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func TestCopilotSettingsKeepTheKeyOnTheMachineAndNeverSayIt(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	rec := copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot",
		`{"openai_key":"`+testCopilotKey+`","eagerness":"auto","extra_instructions":"叫我 Sean"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("write: %d %s", rec.Code, rec.Body)
	}
	for _, r := range []*httptest.ResponseRecorder{rec, copilotCall(t, s, s.copilotSettingsRoute, http.MethodGet, "/v1/settings/copilot", "")} {
		if strings.Contains(r.Body.String(), "test0000") {
			t.Fatalf("an answer carried the key: %s", r.Body)
		}
		var got copilotSettingsAnswer
		if err := json.Unmarshal(r.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if !got.KeySet || got.KeyHint != "LAST" || got.Eagerness != "auto" || got.Model != "gpt-realtime-mini" ||
			got.ExtraInstructions != "叫我 Sean" || !got.Briefing || got.BriefingAssistant != "claude" {
			t.Fatalf("answer: %+v", got)
		}
	}
	info, err := os.Stat(filepath.Join(s.cfg.Dir, "copilot.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("copilot.json is %v, want 0600", info.Mode().Perm())
	}
}

func TestCopilotSettingsRefuseWhatIsNotOffered(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	for body, code := range map[string]string{
		`{"openai_key":"not-a-key"}`: "copilot_bad_key",
		`{"model":"gpt-4o"}`:         "copilot_bad_model",
		`{"voice":"fable"}`:          "copilot_bad_voice",
		`{"something":"else"}`:       "bad_request",
	} {
		rec := copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", body)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%s: %d %s, want %s", body, rec.Code, rec.Body, code)
		}
	}
	if _, err := os.Stat(filepath.Join(s.cfg.Dir, "copilot.json")); !os.IsNotExist(err) {
		t.Fatal("a refused change wrote the file")
	}
}

func fakeOpenAI(t *testing.T, status int, answer string) *[]map[string]any {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/realtime/client_secrets" || r.Header.Get("Authorization") != "Bearer "+testCopilotKey {
			t.Errorf("minted at %s with %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		seen = append(seen, body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	was := copilotOpenAI
	copilotOpenAI = srv.URL
	t.Cleanup(func() { copilotOpenAI = was; srv.Close() })
	return &seen
}

func TestACallStartsWithAClientSecretMintedOnTheMachine(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`","model":"gpt-realtime","voice":"cedar"}`)
	seen := fakeOpenAI(t, http.StatusOK, `{"value":"ek_abc123","expires_at":1794500000,"session":{}}`)
	rec := copilotCall(t, s, s.copilotSessionRoute, http.MethodPost, "/v1/voice/copilot-session", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got copilotSessionAnswer
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.ClientSecret != "ek_abc123" || got.ExpiresAt != 1794500000 || got.Model != "gpt-realtime" || got.Eagerness != "low" || got.Voice != "cedar" {
		t.Fatalf("answer: %+v", got)
	}
	if strings.Contains(rec.Body.String(), "test0000") {
		t.Fatal("the session answer carried the key")
	}
	if len(*seen) != 1 {
		t.Fatalf("minted %d times", len(*seen))
	}
	sent := (*seen)[0]
	if sess, _ := sent["session"].(map[string]any); sess["model"] != "gpt-realtime" || sess["type"] != "realtime" {
		t.Fatalf("minted for %v", sent)
	}
	if exp, _ := sent["expires_after"].(map[string]any); exp["seconds"] != float64(600) {
		t.Fatalf("lifetime %v", sent["expires_after"])
	}
}

func TestACallWithoutAKeyIsATypedRefusal(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	rec := copilotCall(t, s, s.copilotSessionRoute, http.MethodPost, "/v1/voice/copilot-session", "")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "copilot_key_missing") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestARejectedKeyIsSaidWithoutEchoingIt(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`"}`)
	fakeOpenAI(t, http.StatusUnauthorized, `{"error":{"message":"Incorrect API key provided: sk-proj-****LAST.","code":"invalid_api_key"}}`)
	rec := copilotCall(t, s, s.copilotSessionRoute, http.MethodPost, "/v1/voice/copilot-session", "")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "copilot_key_rejected") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), "sk-") {
		t.Fatalf("the refusal echoed the key: %s", rec.Body)
	}
}
