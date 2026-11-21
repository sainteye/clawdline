package http

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/config"
)

func TestRealtimeDictationRequiresOptInAndMintsOnlyATranscriptionGrant(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	open := func() (int, string) {
		rec := copilotCall(t, s, s.voiceLiveTranscriptionRoute, http.MethodPost, "/v1/voice/live-transcription", "")
		return rec.Code, rec.Body.String()
	}
	seen := fakeOpenAI(t, http.StatusOK, `{"value":"fixture-transcription-grant","expires_at":1794500000}`)
	if status, body := open(); status != http.StatusConflict || !strings.Contains(body, "voice_realtime_disabled") {
		t.Fatalf("default opened dictation: %d %s", status, body)
	}
	if invalid := voiceSettingsCall(t, s, http.MethodPost, `{"voice_transcription":"other"}`); invalid.Code != http.StatusBadRequest {
		t.Fatalf("unknown engine: %d %s", invalid.Code, invalid.Body)
	}
	if set := voiceSettingsCall(t, s, http.MethodPost, `{"voice_transcription":"openai_realtime"}`); set.Code != http.StatusOK {
		t.Fatalf("voice engine: %d %s", set.Code, set.Body)
	}
	if status, body := open(); status != http.StatusConflict || !strings.Contains(body, "copilot_key_missing") || len(*seen) != 0 {
		t.Fatalf("missing key: %d %s, requests: %d", status, body, len(*seen))
	}

	if set := copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`"}`); set.Code != http.StatusOK {
		t.Fatalf("API key setup: %d %s", set.Code, set.Body)
	}
	if _, err := s.settingsFile().Set(map[string]any{"voice_language": "zh-TW"}); err != nil {
		t.Fatal(err)
	}
	status, body := open()
	var grant map[string]any
	if err := json.Unmarshal([]byte(body), &grant); err != nil || status != http.StatusOK ||
		!reflect.DeepEqual(grant, map[string]any{"client_secret": "fixture-transcription-grant"}) {
		t.Fatalf("browser grant: %d %s, %v", status, body, err)
	}
	if len(*seen) != 1 {
		t.Fatalf("minted %d times", len(*seen))
	}
	want := map[string]any{
		"type": "transcription",
		"audio": map[string]any{"input": map[string]any{
			"transcription":  map[string]any{"model": "gpt-live-transcribe", "languages": []any{"zh-tw"}},
			"turn_detection": nil,
		}},
	}
	if got := (*seen)[0]["session"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("provider session = %#v, want %#v", got, want)
	}
	if setting := voiceSettingsCall(t, s, http.MethodGet, ""); !strings.Contains(setting.Body.String(), `"voice_transcription":"openai_realtime"`) ||
		!strings.Contains(setting.Body.String(), `"openai_key_set":true`) || strings.Contains(setting.Body.String(), testCopilotKey) {
		t.Fatalf("settings readback: %s", setting.Body)
	}
	if set := voiceSettingsCall(t, s, http.MethodPost, `{"voice_transcription":"whisper"}`); set.Code != http.StatusOK {
		t.Fatalf("reset engine: %d %s", set.Code, set.Body)
	}
	if status, _ := open(); status != http.StatusConflict || len(*seen) != 1 {
		t.Fatalf("switching back still opened Realtime: %d, requests: %d", status, len(*seen))
	}
}
