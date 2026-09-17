package http

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/whisper"
	"github.com/sainteye/clawdline-go/internal/config"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/auth"
)

// voiceCall posts one body as a device with the capabilities named. The gate
// is not in the way — its own refusals have their own tests — so what is being
// asked here is what this route decides for itself.
func voiceCall(t *testing.T, s *Server, key string, caps auth.Caps, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/voice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	req = req.WithContext(context.WithValue(req.Context(), accessKey{}, access{
		verdict: auth.Verdict{Allowed: true, Device: "test-device", Caps: caps},
	}))
	rec := httptest.NewRecorder()
	s.voiceRoute(rec, req)
	return rec
}

func refusalOf(t *testing.T, rec *httptest.ResponseRecorder) contract.VoiceRefusal {
	t.Helper()
	var out contract.VoiceRefusal
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%d: %s", rec.Code, rec.Body)
	}
	return out
}

// A body of `n` seconds of loud enough audio, as the browser encodes it.
func voiceBody(seconds float64, rate int) string {
	samples := make([]byte, int(float64(whisper.Rate)*seconds)*2)
	for i := 0; i+1 < len(samples); i += 2 {
		v := 9000
		if (i/80)%2 == 0 {
			v = -9000
		}
		samples[i] = byte(uint16(int16(v)) & 0xFF)
		samples[i+1] = byte(uint16(int16(v)) >> 8)
	}
	return fmt.Sprintf(`{"rate":%d,"audio":%q}`, rate, base64.StdEncoding.EncodeToString(samples))
}

func voiceServer(t *testing.T) *Server {
	t.Helper()
	return &Server{cfg: config.Config{Dir: filepath.Join(t.TempDir(), "clawdline-next")}}
}

// Everything this route decides before a second of anybody's CPU is spent.
func TestVoiceRefusesWhatItCannotRead(t *testing.T) {
	s := voiceServer(t)
	send := auth.NewCaps(auth.Read, auth.Send)

	// **Write, not read**, and the reason is not that this writes anything: a
	// device that may only read has nowhere to put a sentence, and
	// transcribing costs this machine every core it has.
	if rec := voiceCall(t, s, "k1", auth.NewCaps(auth.Read), voiceBody(1, 16000)); rec.Code != http.StatusForbidden {
		t.Fatalf("a reader was let in: %d %s", rec.Code, rec.Body)
	}
	// A retried POST must not be a second read.
	if rec := voiceCall(t, s, "", send, voiceBody(1, 16000)); rec.Code != http.StatusBadRequest {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}

	cases := []struct {
		why  string
		key  string
		body string
		code int
	}{
		// Checked rather than resampled: a body that names 48000 has sent
		// something that would transcribe as a voice three times too fast.
		{"the wrong rate", "k2", voiceBody(1, 48000), http.StatusBadRequest},
		{"no audio", "k3", `{"rate":16000,"audio":""}`, http.StatusBadRequest},
		{"not base64", "k4", `{"rate":16000,"audio":"not base64 at all !!"}`, http.StatusBadRequest},
		{"not a body", "k5", `{`, http.StatusBadRequest},
		// A quarter of a second is a tap, five minutes is the ceiling.
		{"a tap", "k6", voiceBody(0.1, 16000), http.StatusBadRequest},
		{"eight minutes", "k7", voiceBody(480, 16000), http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := voiceCall(t, s, c.key, send, c.body)
		if rec.Code != c.code {
			t.Fatalf("%s: %d %s", c.why, rec.Code, rec.Body)
		}
		if got := refusalOf(t, rec).Error; got != "bad_request" {
			t.Fatalf("%s: error = %q", c.why, got)
		}
	}

	if rec := voiceCall(t, s, "k8", send, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty body: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/voice", nil)
	rec := httptest.NewRecorder()
	s.voiceRoute(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", rec.Code)
	}
}

// The two halves of "there is no Whisper here", told apart on the wire —
// asked with search paths of the test's own, so what is installed on this
// machine is neither moved nor consulted.
func TestVoiceSaysWhichHalfOfWhisperIsMissing(t *testing.T) {
	send := auth.NewCaps(auth.Read, auth.Send)
	empty, bins := t.TempDir(), t.TempDir()

	s := voiceServer(t)
	useEngine(t, func(*Server) whisper.Transcriber {
		return whisper.Transcriber{BinaryDirs: []string{empty}, ModelDirs: []string{empty},
			LookPath: func(string) (string, error) { return "", errNoPath }}
	})
	rec := voiceCall(t, s, "n1", send, voiceBody(1, 16000))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no binary: %d %s", rec.Code, rec.Body)
	}
	if got := refusalOf(t, rec); got.Error != "no_whisper" || got.Reason != "no_binary" {
		t.Fatalf("no binary: %+v", got)
	}

	// `brew install whisper-cpp` leaves you here, and "no whisper" would send
	// somebody to check the thing they already did.
	fakeBinary(t, filepath.Join(bins, "whisper-cli"))
	useEngine(t, func(*Server) whisper.Transcriber {
		return whisper.Transcriber{BinaryDirs: []string{bins}, ModelDirs: []string{empty},
			LookPath: func(string) (string, error) { return "", errNoPath }}
	})
	rec = voiceCall(t, s, "n2", send, voiceBody(1, 16000))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no model: %d %s", rec.Code, rec.Body)
	}
	if got := refusalOf(t, rec); got.Error != "no_whisper" || got.Reason != "no_model" {
		t.Fatalf("no model: %+v", got)
	}

	// **A 503 is not filed.** It is about this machine at this moment rather
	// than about the request — whisper gets installed — so the same key asked
	// again is answered again rather than replayed.
	write, models := t.TempDir(), t.TempDir()
	fakeBinary(t, filepath.Join(write, "whisper-cli"))
	fakeModel(t, filepath.Join(models, "ggml-large-v3.bin"))
	useEngine(t, func(*Server) whisper.Transcriber {
		return whisper.Transcriber{BinaryDirs: []string{write}, ModelDirs: []string{models},
			LookPath: func(string) (string, error) { return "", errNoPath },
			TempDir:  t.TempDir(),
			Run: func(context.Context, string, ...string) ([]byte, error) {
				return []byte("這一段有聽到\n"), nil
			}}
	})
	rec = voiceCall(t, s, "n2", send, voiceBody(1, 16000))
	if rec.Code != http.StatusOK {
		t.Fatalf("the refusal was cached: %d %s", rec.Code, rec.Body)
	}
	var out contract.VoiceResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Text != "這一段有聽到" {
		t.Fatalf("result = %s (%v)", rec.Body, err)
	}
}

// A transcript is worth keeping under its key: the same key asked twice reads
// once. **Nothing heard is a 200** — the machine heard the recording and there
// were no words in it, and a red banner for that would be the page reporting a
// fault that did not occur.
func TestVoiceFilesATranscriptAndAnswersSilenceWithNothing(t *testing.T) {
	send := auth.NewCaps(auth.Read, auth.Send)
	bins, models := t.TempDir(), t.TempDir()
	fakeBinary(t, filepath.Join(bins, "whisper-cli"))
	fakeModel(t, filepath.Join(models, "ggml-large-v3.bin"))

	runs := 0
	s := voiceServer(t)
	useEngine(t, func(*Server) whisper.Transcriber {
		return whisper.Transcriber{BinaryDirs: []string{bins}, ModelDirs: []string{models},
			LookPath: func(string) (string, error) { return "", errNoPath },
			TempDir:  t.TempDir(),
			Run: func(context.Context, string, ...string) ([]byte, error) {
				runs++
				return []byte(" 和音，和音，和音，和音，\n"), nil
			}}
	})
	for i := 0; i < 2; i++ {
		rec := voiceCall(t, s, "same-key", send, voiceBody(1, 16000))
		if rec.Code != http.StatusOK {
			t.Fatalf("pass %d: %d %s", i, rec.Code, rec.Body)
		}
		var out contract.VoiceResult
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		// A groove is not a sentence, and "nothing" is an answer to "what was
		// said".
		if out.Text != "" {
			t.Fatalf("pass %d: text = %q", i, out.Text)
		}
	}
	if runs != 1 {
		t.Fatalf("whisper ran %d times for one key", runs)
	}
}

var errNoPath = fmt.Errorf("not on PATH")

// useEngine points the route at a transcriber of this test's own for the
// length of the test, and puts the real one back afterwards.
func useEngine(t *testing.T, build func(*Server) whisper.Transcriber) {
	t.Helper()
	was := voiceEngine
	voiceEngine = build
	t.Cleanup(func() { voiceEngine = was })
}

func fakeBinary(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func fakeModel(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, make([]byte, 2048), 0o644); err != nil {
		t.Fatal(err)
	}
}
