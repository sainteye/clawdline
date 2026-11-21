package http

import (
	"context"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/capacity"
)

// A preview has no stored receipt or LLM pass. It may use the Whisper slot
// only while it is idle, and the stopped recording still owns the final draft.
func (s *Server) voicePreviewRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return
	}
	if !maySend(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "This device may read, and not send.")
		return
	}
	samples, ok := voiceSamplesWithin(w, r, capacity.MaximumWhisperPreviewBodyBytes, capacity.WhisperPreviewSeconds*time.Second,
		"Send a shorter preview recording.")
	if !ok {
		return
	}
	if !voiceTryPreview() {
		writeRefusal(w, http.StatusTooManyRequests, "busy", "Whisper is busy with another recording.")
		return
	}
	defer voiceLeave()

	started := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), capacity.MaximumWhisperPreviewMilliseconds*time.Millisecond)
	defer cancel()
	text, err := voiceEngine(s).Transcribe(ctx, samples)
	if err != nil {
		writeVoiceFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, contract.VoiceResult{Text: text, Ms: time.Since(started).Milliseconds()})
}

func voiceTryPreview() bool {
	voiceQueued.Lock()
	defer voiceQueued.Unlock()
	if voiceQueued.n != 0 {
		return false
	}
	voiceQueued.n++
	return true
}
