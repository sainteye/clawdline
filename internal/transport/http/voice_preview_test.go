package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/whisper"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/auth"
)

func TestVoicePreviewUsesOnlyTheIdleWhisperSlotAndSkipsPolish(t *testing.T) {
	s := voiceServer(t)
	bins, models := t.TempDir(), t.TempDir()
	fakeBinary(t, filepath.Join(bins, "whisper-cli"))
	fakeModel(t, filepath.Join(models, "ggml-large-v3.bin"))
	if _, err := s.settingsFile().Set(map[string]any{"voice_polish": true}); err != nil {
		t.Fatal(err)
	}
	runs := 0
	useEngine(t, func(*Server) whisper.Transcriber {
		return whisper.Transcriber{BinaryDirs: []string{bins}, ModelDirs: []string{models}, TempDir: t.TempDir(),
			Run: func(context.Context, string, ...string) ([]byte, error) {
				runs++
				return []byte("第一段預覽"), nil
			}}
	})
	call := func(caps auth.Caps, seconds float64) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/voice/preview", strings.NewReader(voiceBody(seconds, 16000)))
		req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
			verdict: auth.Verdict{Allowed: true, Device: "test-device", Caps: caps},
		}))
		rec := httptest.NewRecorder()
		s.voicePreviewRoute(rec, req)
		return rec
	}
	send := auth.NewCaps(auth.Read, auth.Send)
	if got := call(auth.NewCaps(auth.Read), 3); got.Code != http.StatusForbidden {
		t.Fatalf("read-only preview = %d %s", got.Code, got.Body)
	}
	if got := call(send, 3.01); got.Code != http.StatusBadRequest {
		t.Fatalf("too-long preview = %d %s", got.Code, got.Body)
	}
	if !voiceEnqueue() {
		t.Fatal("could not occupy normal transcription slot")
	}
	busy := call(send, 3)
	voiceLeave()
	if busy.Code != http.StatusTooManyRequests || runs != 0 {
		t.Fatalf("busy preview = %d %s (Whisper runs %d)", busy.Code, busy.Body, runs)
	}
	rec := call(send, 3)
	var transcript contract.VoiceResult
	if err := json.Unmarshal(rec.Body.Bytes(), &transcript); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "no-store" ||
		transcript.Text != "第一段預覽" || transcript.Polish != nil || runs != 1 {
		t.Fatalf("preview = %d %+v (Whisper runs %d)", rec.Code, transcript, runs)
	}
}
