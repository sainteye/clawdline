package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/domain/copilot"
)

// The GPT-Live side of the voice copilot (docs/voice.md, "GPT-Live calls").
//
// GPT-Live has no client secret: the server that holds the key creates the
// session and exchanges the browser's WebRTC offer for OpenAI's answer. So a
// GPT-Live call start carries the offer and the live model's short
// instructions to this machine, which posts them to /v1/live/sessions with
// its key and hands back only the answer and the session's id. The backend's
// long instructions and the browser's tools follow over the call's own data
// channel (`session.update`), so this body stays small enough for Cloud.

// copilotLiveBodyLimit bounds a GPT-Live call start's body: the offer, the
// instructions and the JSON around them.
const copilotLiveBodyLimit = copilot.MaxLiveSDPBytes + copilot.MaxLiveInstructionsBytes + 4096

type copilotLiveRequest struct {
	Call         string `json:"call,omitempty"`
	SDP          string `json:"sdp"`
	Instructions string `json:"instructions"`
}

type copilotLiveAnswer struct {
	SessionID    string `json:"session_id"`
	SDP          string `json:"sdp"`
	Model        string `json:"model"`
	BackendModel string `json:"backend_model"`
	Voice        string `json:"voice"`
}

// copilotLiveRoute is POST /v1/voice/copilot-live. A paired device needs
// Send (gate.go): the session it opens is billed to the machine's key.
func (s *Server) copilotLiveRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "a GPT-Live call start is application/json")
		return
	}
	var req copilotLiveRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, copilotLiveBodyLimit))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "That body is not a GPT-Live call start.")
		return
	}
	if !strings.HasPrefix(req.SDP, "v=0") || len(req.SDP) > copilot.MaxLiveSDPBytes {
		writeRefusal(w, http.StatusBadRequest, "copilot_live_bad_offer", "A GPT-Live call start carries the browser's WebRTC offer.")
		return
	}
	if len(req.Instructions) > copilot.MaxLiveInstructionsBytes || !utf8.ValidString(req.Instructions) {
		writeRefusal(w, http.StatusBadRequest, "copilot_live_bad_instructions", "The GPT-Live instructions are too long.")
		return
	}
	if req.Call != "" && !copilotJournalCall.MatchString(req.Call) {
		writeRefusal(w, http.StatusBadRequest, "invalid_body", "A usage call names one call journal.")
		return
	}
	v, err := s.readCopilot()
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "copilot_settings_unreadable", "The voice copilot settings on this machine cannot be read.")
		return
	}
	if strings.TrimSpace(v.Key) == "" {
		writeRefusal(w, http.StatusConflict, "copilot_key_missing", "No OpenAI API key is set for the voice copilot on this machine. Add it in Settings › Voice copilot.")
		return
	}
	e := v.Effective()
	ctx, cancel := context.WithTimeout(r.Context(), copilotMintTimeout)
	defer cancel()
	id, answer, status, detail := openLiveSession(ctx, v.Key, e.Voice, req)
	if answer == "" {
		code := "copilot_live_failed"
		if status == http.StatusUnauthorized {
			code = "copilot_key_rejected"
		}
		writeRawRefusal(w, http.StatusBadGateway, code, "OpenAI did not open a GPT-Live call: "+detail)
		return
	}
	if req.Call != "" {
		now := time.Now()
		u := copilotUsageSnapshot{Seq: -1, Started: now.UnixMilli(), Engine: "live", Model: copilot.LiveModel, USD: 0.0125, Parts: map[string]float64{"voice": 0.0125}, Seconds: 15, Priced: true}
		usage, _ := json.Marshal(u)
		line, _ := json.Marshal(struct {
			Call string `json:"call"`
			copilotJournalEntry
		}{req.Call, copilotJournalEntry{At: now.UnixMilli(), Kind: "notice", Text: "usage_snapshot", Tool: usage}})
		if err := s.appendCopilotJournal(now, append(line, '\n')); err != nil {
			log.Printf("voice initialization usage: %v", err)
		}
	}
	writeJSON(w, copilotLiveAnswer{SessionID: id, SDP: answer, Model: copilot.LiveModel, BackendModel: copilot.LiveBackendModel, Voice: e.Voice})
}

// openLiveSession creates one GPT-Live session with Responses delegation and
// exchanges the offer. The detail it returns is OpenAI's own message, which
// never contains the key.
func openLiveSession(ctx context.Context, key, voice string, req copilotLiveRequest) (string, string, int, string) {
	session := map[string]any{
		"model": copilot.LiveModel,
		"audio": map[string]any{"output": map[string]any{"voice": voice}},
		// The browser runs every tool the backend calls; the backend's own
		// instructions and tools arrive by session.update once it starts.
		"delegation": map[string]any{"type": "responses", "responses": map[string]any{"model": copilot.LiveBackendModel}},
	}
	if strings.TrimSpace(req.Instructions) != "" {
		session["instructions"] = req.Instructions
	}
	body, _ := json.Marshal(map[string]any{"session": session, "transport": map[string]any{"type": "webrtc", "sdp": req.SDP}})
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, copilotOpenAI+"/v1/live/sessions", bytes.NewReader(body))
	if err != nil {
		return "", "", 0, "the request could not be built"
	}
	hreq.Header.Set("Authorization", "Bearer "+key)
	hreq.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", 0, "OpenAI did not answer in time"
		}
		return "", "", 0, "OpenAI could not be reached"
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var answer struct {
		Session struct {
			ID string `json:"id"`
		} `json:"session"`
		Transport struct {
			SDP string `json:"sdp"`
		} `json:"transport"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)
	if res.StatusCode/100 != 2 || answer.Transport.SDP == "" {
		detail := strings.TrimSpace(answer.Error.Message)
		if detail == "" {
			detail = res.Status
		}
		return "", "", res.StatusCode, detail
	}
	return answer.Session.ID, answer.Transport.SDP, res.StatusCode, ""
}
