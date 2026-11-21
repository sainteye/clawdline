package http

import (
	"context"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
)

const voiceRealtimeEngine = "openai_realtime"
const voiceRealtimeModel = "gpt-live-transcribe"

func voiceTranscriptionEngine(values nextconfig.Values) string {
	if engine, _ := values.String("voice_transcription"); engine == voiceRealtimeEngine {
		return engine
	}
	return "whisper"
}

func liveTranscriptionSession(plan whisper.Plan) map[string]any {
	transcription := map[string]any{"model": voiceRealtimeModel}
	language := plan.Code
	if language == "zh" {
		switch plan.Script {
		case "Hant":
			language = "zh-tw"
		case "Hans":
			language = "zh-cn"
		}
	}
	if language != "" && language != "auto" {
		transcription["languages"] = []string{language}
	}
	return map[string]any{
		"type": "transcription",
		"audio": map[string]any{"input": map[string]any{
			"transcription":  transcription,
			"turn_detection": nil,
		}},
	}
}

// The browser presents this grant to OpenAI over HTTPS before sending any
// microphone audio. The machine's long-lived API key stays on the machine.
func (s *Server) voiceLiveTranscriptionRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return
	}
	file := s.settingsFile()
	values, err := file.Read()
	if err != nil {
		writeSettingsFailure(w, file, err)
		return
	}
	if voiceTranscriptionEngine(values) != voiceRealtimeEngine {
		writeRefusal(w, http.StatusConflict, "voice_realtime_disabled", "OpenAI Realtime transcription is disabled in Voice input settings.")
		return
	}
	config, err := s.readCopilot()
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "copilot_settings_unreadable", "The OpenAI settings on this machine cannot be read.")
		return
	}
	if strings.TrimSpace(config.Key) == "" {
		writeRefusal(w, http.StatusConflict, "copilot_key_missing", "Add an OpenAI API key in Voice assistant settings.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), copilotMintTimeout)
	defer cancel()
	secret, _, status, _ := mintRealtimeSecret(ctx, config.Key, liveTranscriptionSession(s.whisper().Plan()))
	if secret == "" {
		if status == http.StatusUnauthorized {
			writeRefusal(w, http.StatusBadGateway, "copilot_key_rejected", "OpenAI rejected this machine's API key.")
			return
		}
		writeRefusal(w, http.StatusBadGateway, "voice_realtime_failed", "OpenAI did not open a transcription session.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, struct {
		ClientSecret string `json:"client_secret"`
	}{ClientSecret: secret})
}
