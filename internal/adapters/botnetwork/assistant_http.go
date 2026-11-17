package botnetwork

import (
	"encoding/json"
	"io"
	"net/http"

	app "github.com/sainteye/clawdline/internal/app/botnetwork"
	wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

// AssistantRequest preserves exact signed plaintext bytes through JSON framing.
// Body is a UTF-8 string containing the narrow operation's JSON payload.
type AssistantRequest struct {
	Call wire.AssistantCall `json:"call"`
	Body string             `json:"body"`
}

// AssistantHandler has no viewer-auth fallback. Without a configured service or
// gateway trust, this dedicated protocol returns a typed unavailable refusal.
type AssistantHandler struct{ Service *app.AssistantService }

func (h AssistantHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost || r.URL.Path != "/v1/bot-assistant/call" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if h.Service == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": string(wire.AssistantUnavailable)})
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, int64(wire.MaxBodyBytes*2)))
	decoder.DisallowUnknownFields()
	var input AssistantRequest
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": string(wire.AssistantInvalid)})
		return
	}
	response, err := h.Service.Receive(r.Context(), input.Call, []byte(input.Body))
	if err != nil {
		status := http.StatusForbidden
		if err == wire.AssistantUnavailable {
			status = http.StatusServiceUnavailable
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(response)
}
