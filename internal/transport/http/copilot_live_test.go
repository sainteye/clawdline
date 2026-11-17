package http

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/config"
)

// fakeLiveOpenAI answers /v1/live/sessions as OpenAI documents it: 201 with
// the session's id and the SDP answer.
func fakeLiveOpenAI(t *testing.T, status int, answer string) *[]map[string]any {
	t.Helper()
	var seen []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/live/sessions" || r.Header.Get("Authorization") != "Bearer "+testCopilotKey {
			t.Errorf("opened at %s with %q", r.URL.Path, r.Header.Get("Authorization"))
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

func liveCall(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/voice/copilot-live", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.copilotLiveRoute(rec, req)
	return rec
}

func TestAGPTLiveCallIsOpenedByTheMachineWithItsKey(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`","voice":"cedar","engine":"live"}`)
	seen := fakeLiveOpenAI(t, http.StatusCreated, `{"session":{"id":"sess_1"},"transport":{"type":"webrtc","sdp":"v=0 answer"}}`)
	rec := liveCall(t, s, `{"sdp":"v=0 offer","instructions":"Speak briefly."}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	var got copilotLiveAnswer
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.SessionID != "sess_1" || got.SDP != "v=0 answer" || got.Model != "gpt-live-1" || got.Voice != "cedar" {
		t.Fatalf("answer: %+v", got)
	}
	if strings.Contains(rec.Body.String(), "test0000") {
		t.Fatal("the answer carried the key")
	}
	if len(*seen) != 1 {
		t.Fatalf("opened %d times", len(*seen))
	}
	sent := (*seen)[0]
	sess, _ := sent["session"].(map[string]any)
	transport, _ := sent["transport"].(map[string]any)
	delegation, _ := sess["delegation"].(map[string]any)
	backend, _ := delegation["responses"].(map[string]any)
	audio, _ := sess["audio"].(map[string]any)
	output, _ := audio["output"].(map[string]any)
	if sess["model"] != "gpt-live-1" || sess["instructions"] != "Speak briefly." || delegation["type"] != "responses" || backend["model"] != "gpt-6-luna" || output["voice"] != "cedar" {
		t.Fatalf("session sent: %v", sess)
	}
	if transport["type"] != "webrtc" || transport["sdp"] != "v=0 offer" {
		t.Fatalf("transport sent: %v", transport)
	}
	if settings := copilotCall(t, s, s.copilotSettingsRoute, http.MethodGet, "/v1/settings/copilot", ""); !strings.Contains(settings.Body.String(), `"engine":"live"`) {
		t.Fatalf("settings: %s", settings.Body)
	}
}

func TestAGPTLiveStartIsRefusedBeforeOpenAIIsAsked(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	seen := fakeLiveOpenAI(t, http.StatusCreated, `{}`)
	if rec := liveCall(t, s, `{"sdp":"v=0","instructions":""}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "copilot_key_missing") {
		t.Fatalf("no key: %d %s", rec.Code, rec.Body)
	}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`"}`)
	for body, code := range map[string]string{
		`{"sdp":"not an offer"}`: "copilot_live_bad_offer",
		`{"sdp":"v=0","instructions":"` + strings.Repeat("x", 25<<10) + `"}`: "copilot_live_bad_instructions",
		`{"sdp":"v=0","extra":1}`: "bad_request",
	} {
		if rec := liveCall(t, s, body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), code) {
			t.Errorf("%.40s: %d %s, want %s", body, rec.Code, rec.Body, code)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("OpenAI was asked %d times", len(*seen))
	}
	if rec := copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"engine":"telepathy"}`); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "copilot_bad_engine") {
		t.Fatalf("bad engine: %d %s", rec.Code, rec.Body)
	}
}

func TestARejectedGPTLiveStartSaysOpenAIsReason(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`"}`)
	fakeLiveOpenAI(t, http.StatusForbidden, `{"error":{"message":"This project does not have access to gpt-live-1."}}`)
	rec := liveCall(t, s, `{"sdp":"v=0"}`)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "copilot_live_failed") || !strings.Contains(rec.Body.String(), "does not have access") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
}

func TestALiveCreationKeepsItsInitializationEstimateEvenWithoutAConnectedPhone(t *testing.T) {
	s := &Server{cfg: config.Config{Dir: t.TempDir()}}
	copilotCall(t, s, s.copilotSettingsRoute, http.MethodPost, "/v1/settings/copilot", `{"openai_key":"`+testCopilotKey+`"}`)
	fakeLiveOpenAI(t, http.StatusCreated, `{"session":{"id":"sess_1"},"transport":{"type":"webrtc","sdp":"v=0 answer"}}`)
	rec := liveCall(t, s, `{"sdp":"v=0 offer","call":"call-init-only"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	history := s.copilotUsageHistory(time.Now())
	if len(history.Days) != 1 {
		t.Fatalf("%+v", history)
	}
	day := history.Days[0]
	if day.Calls != 1 || day.Partial != 1 || day.Seconds != 15 || day.USD != 0.0125 {
		t.Fatalf("%+v", day)
	}
}
