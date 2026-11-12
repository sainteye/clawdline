package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// The voice copilot's Decision API (docs/voice.md, "Which Session the person
// means").
//
// A read whose Session the call names by neither id nor whole name sends the
// person's transcribed sentence here. The machine reads its own Session list,
// as the briefing does, and asks a fast text model on the person's key which
// one the sentence means. It answers one id, or the few it cannot tell apart,
// or neither; the call uses only an id that is in its own list.
//
// Measured live on 2026-11-12 with this machine's Sessions and eleven spoken
// descriptions: the voice model alone read the right Session for 7; with this
// decision on the sentence it heard, 10, at a median of 0.7 s per decision.

const (
	// copilotDecisionModel answers in well under a second on short input.
	copilotDecisionModel = "gpt-4.1-mini"
	// maxCopilotDecisionSaidRunes bounds the sentence: one spoken turn.
	maxCopilotDecisionSaidRunes = 1000
	// maxCopilotDecisionDuration bounds the one outbound request; a slower
	// decision is abandoned and the copilot chooses from the list itself.
	maxCopilotDecisionDuration = 8 * time.Second
	// copilotDecisionBodyLimit is the route's request: the sentence, and the
	// Session list a page showing every machine hands over.
	copilotDecisionBodyLimit = 64 << 10
	// copilotDecisionTextRunes is how much of a delivery or a question each
	// Session is shown by.
	copilotDecisionTextRunes = 200
	// copilotDecisionCandidates is how many Sessions one decision may name:
	// the few it cannot tell apart, or the several a question is about.
	copilotDecisionCandidates = 5
	// maxCopilotDecisionSessions bounds a list the call hands over.
	maxCopilotDecisionSessions = 200
)

type copilotDecisionOption struct {
	N     int    `json:"n"`
	Label string `json:"label"`
}
type copilotDecisionSession struct {
	MenuFingerprint string                  `json:"menu_fingerprint,omitempty"`
	Options         []copilotDecisionOption `json:"options,omitempty"`
	Conversation    string                  `json:"conversation,omitempty"`
	Machine         string                  `json:"machine,omitempty"`
	ID              string                  `json:"id"`
	Label           string                  `json:"label"`
	State           string                  `json:"state"`
	Assistant       string                  `json:"assistant"`
	Delivered       string                  `json:"delivered,omitempty"`
	Question        string                  `json:"question,omitempty"`
}

type copilotDecisionAnswer struct {
	ID         string   `json:"id"`
	Candidates []string `json:"candidates"`
	// Many says the sentence asks about several Sessions at once ("他們"),
	// so the candidates are all of them, not a choice to put to the person.
	Many bool `json:"many"`
}

const copilotDecisionPrompt = "下面是使用者在語音通話裡說的一句話（語音辨識的結果，會有同音字、錯字、中英混雜，或只講主題），裡面提到他電腦上的一個 AI 程式開發 Session。從清單裡挑出他指的那一個，依名稱、狀態、助理與交付摘要的意思判斷，不是只比對字面。很確定就填 id；有兩三個都說得通就把它們放進 candidates、id 填 null；清單裡沒有相符的、或這句話沒有指向任何特定 Session（例如只說「它」「這個」或在問整體狀況），就兩個都留空。如果他一次問的是好幾個 Session（例如「它們」「那幾個」「處理中的那些」「最近做完的」），把符合的都放進 candidates（最多五個）、id 填 null、many 填 true；只問一個時 many 填 false。"

// copilotDecisionRequest is the Chat Completions body for one sentence.
func copilotDecisionRequest(said string, sessions []copilotDecisionSession) map[string]any {
	user, _ := json.Marshal(map[string]any{"said": said, "sessions": sessions})
	return map[string]any{
		"model":       copilotDecisionModel,
		"temperature": 0,
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "session_choice",
				"strict": true,
				"schema": map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties": map[string]any{
						"id":         map[string]any{"type": []string{"string", "null"}},
						"candidates": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"many":       map[string]any{"type": "boolean"},
					},
					"required": []string{"id", "candidates", "many"},
				},
			},
		},
		"messages": []map[string]string{
			{"role": "system", "content": copilotDecisionPrompt},
			{"role": "user", "content": string(user)},
		},
	}
}

// copilotDecisionSessions is this machine's own Session list as the decision
// is shown it.
func (s *Server) copilotDecisionSessions(ctx context.Context) ([]copilotDecisionSession, bool) {
	var list struct {
		Sessions []copilotRow `json:"sessions"`
	}
	if !copilotOwnRead(ctx, http.HandlerFunc(s.sessions), "/v1/sessions", &list) {
		return nil, false
	}
	out := make([]copilotDecisionSession, 0, len(list.Sessions))
	for _, r := range list.Sessions {
		d := copilotDecisionSession{ID: r.ID, Label: r.Label, State: r.State, Assistant: r.Assistant}
		if r.Disposition != nil {
			d.Delivered = clipRunes(r.Disposition.Title, copilotDecisionTextRunes)
		}
		if r.Menu != nil {
			d.Question = clipRunes(r.Menu.Question, copilotDecisionTextRunes)
		}
		out = append(out, d)
	}
	return out, true
}

func (s *Server) copilotDecideRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
		return
	}
	var body struct {
		Said string `json:"said"`
		// Sessions is the list the call itself reads when it is not this
		// machine's own: a page showing every machine names each machine's
		// Sessions, and a decision over this machine's alone named none of them.
		Sessions []copilotDecisionSession `json:"sessions"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, copilotDecisionBodyLimit+1))
	if err != nil || len(raw) > copilotDecisionBodyLimit || json.Unmarshal(raw, &body) != nil {
		writeRefusal(w, http.StatusBadRequest, "invalid_body", "Send {\"said\": \"…\"}.")
		return
	}
	said := strings.TrimSpace(body.Said)
	if said == "" {
		writeRefusal(w, http.StatusBadRequest, "said_missing", "Say which sentence to decide on.")
		return
	}
	if utf8.RuneCountInString(said) > maxCopilotDecisionSaidRunes {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "said_too_long", "One spoken sentence is decided on at a time.")
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
	if len(body.Sessions) > maxCopilotDecisionSessions {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "sessions_too_many", "Too many Sessions to decide among at once.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), maxCopilotDecisionDuration)
	defer cancel()
	sessions := body.Sessions
	for i := range sessions {
		sessions[i].Label = clipRunes(sessions[i].Label, copilotDecisionTextRunes)
		sessions[i].Delivered = clipRunes(sessions[i].Delivered, copilotDecisionTextRunes)
		sessions[i].Question = clipRunes(sessions[i].Question, copilotDecisionTextRunes)
	}
	if len(sessions) == 0 {
		own, ok := s.copilotDecisionSessions(ctx)
		if !ok {
			writeRefusal(w, http.StatusServiceUnavailable, "sessions_unreadable", "This machine's Session list could not be read.")
			return
		}
		sessions = own
	}
	if len(sessions) == 0 {
		writeJSON(w, copilotDecisionAnswer{Candidates: []string{}})
		return
	}
	answer, status, detail := decideCopilotSession(ctx, v.Key, said, sessions)
	if detail != "" {
		code := "copilot_decision_failed"
		if status == http.StatusUnauthorized {
			code = "copilot_key_rejected"
		}
		writeRawRefusal(w, http.StatusBadGateway, code, "OpenAI did not decide which Session was meant: "+detail)
		return
	}
	writeJSON(w, answer)
}

// decideCopilotSession asks the decision model once. Only ids from sessions
// survive its answer.
func decideCopilotSession(ctx context.Context, key, said string, sessions []copilotDecisionSession) (copilotDecisionAnswer, int, string) {
	payload, _ := json.Marshal(copilotDecisionRequest(said, sessions))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, copilotOpenAI+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return copilotDecisionAnswer{}, 0, "the request could not be built"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return copilotDecisionAnswer{}, 0, "OpenAI did not answer in time"
		}
		return copilotDecisionAnswer{}, 0, "OpenAI could not be reached"
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &reply)
	if res.StatusCode/100 != 2 || len(reply.Choices) == 0 {
		detail := strings.TrimSpace(reply.Error.Message)
		if detail == "" {
			detail = http.StatusText(res.StatusCode)
		}
		if strings.Contains(detail, "sk-") {
			detail = "incorrect API key (" + reply.Error.Code + ")"
		}
		return copilotDecisionAnswer{}, res.StatusCode, detail
	}
	var chose struct {
		ID         *string  `json:"id"`
		Candidates []string `json:"candidates"`
		Many       bool     `json:"many"`
	}
	if json.Unmarshal([]byte(reply.Choices[0].Message.Content), &chose) != nil {
		return copilotDecisionAnswer{}, res.StatusCode, "the decision was not JSON"
	}
	known := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		known[s.ID] = true
	}
	out := copilotDecisionAnswer{Candidates: []string{}, Many: chose.Many}
	if chose.ID != nil && known[*chose.ID] {
		out.ID = *chose.ID
	}
	for _, id := range chose.Candidates {
		if known[id] && id != out.ID && len(out.Candidates) < copilotDecisionCandidates {
			out.Candidates = append(out.Candidates, id)
		}
	}
	return out, res.StatusCode, ""
}
