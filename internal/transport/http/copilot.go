package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sainteye/clawdline/internal/domain/copilot"
)

// The voice copilot's machine side (docs/voice.md, "Voice copilot").
//
// The OpenAI key is kept in <state>/copilot.json, mode 0600, and never
// leaves this machine: settings answers say only whether a key is set and its
// last four characters, and a phone that starts a call is handed a client
// secret the machine minted from the key, which opens one Realtime call for
// copilot.ClientSecretSeconds and nothing else.

var copilotFileMu sync.Mutex

// copilotOpenAI is where client secrets are minted; tests point it elsewhere.
var copilotOpenAI = "https://api.openai.com"

// copilotMintTimeout bounds the one outbound request a call start makes.
const copilotMintTimeout = 15 * time.Second

func (s *Server) copilotPath() string { return filepath.Join(s.cfg.Dir, "copilot.json") }

func (s *Server) readCopilot() (copilot.Settings, error) {
	copilotFileMu.Lock()
	defer copilotFileMu.Unlock()
	return readCopilotFile(s.copilotPath())
}

func readCopilotFile(path string) (copilot.Settings, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return copilot.Settings{}, nil
	}
	if err != nil {
		return copilot.Settings{}, err
	}
	var v copilot.Settings
	if err := json.Unmarshal(raw, &v); err != nil {
		return copilot.Settings{}, err
	}
	return v, nil
}

// changeCopilot applies one change under the file's lock and writes it
// atomically, so two devices saving at once cannot interleave.
func (s *Server) changeCopilot(c copilot.Change) (copilot.Settings, error) {
	copilotFileMu.Lock()
	defer copilotFileMu.Unlock()
	path := s.copilotPath()
	cur, err := readCopilotFile(path)
	if err != nil {
		return copilot.Settings{}, err
	}
	next, err := copilot.Apply(cur, c)
	if err != nil {
		return copilot.Settings{}, err
	}
	body, err := json.Marshal(next)
	if err != nil {
		return copilot.Settings{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return copilot.Settings{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".copilot-*.json")
	if err != nil {
		return copilot.Settings{}, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return copilot.Settings{}, err
	}
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return copilot.Settings{}, err
	}
	if err := tmp.Close(); err != nil {
		return copilot.Settings{}, err
	}
	if err := os.Rename(name, path); err != nil {
		return copilot.Settings{}, err
	}
	return next, nil
}

// copilotSettingsAnswer is the Settings card: every choice and what the
// machine offers for it, and of the key only whether it is set.
type copilotSettingsAnswer struct {
	KeySet             bool     `json:"key_set"`
	KeyHint            string   `json:"key_hint"`
	Model              string   `json:"model"`
	Eagerness          string   `json:"eagerness"`
	Voice              string   `json:"voice"`
	Speed              string   `json:"speed"`
	ExtraInstructions  string   `json:"extra_instructions"`
	Briefing           bool     `json:"briefing"`
	Brief              bool     `json:"brief"`
	BriefingAssistant  string   `json:"briefing_assistant"`
	WorkEnabled        bool     `json:"work_enabled"`
	WorkAssistant      string   `json:"work_assistant"`
	Models             []string `json:"models"`
	Eagernesses        []string `json:"eagernesses"`
	Voices             []string `json:"voices"`
	Speeds             []string `json:"speeds"`
	BriefingAssistants []string `json:"briefing_assistants"`
	MaxInstructions    int      `json:"max_extra_instructions"`
}

func copilotAnswer(v copilot.Settings) copilotSettingsAnswer {
	e := v.Effective()
	return copilotSettingsAnswer{
		KeySet: strings.TrimSpace(v.Key) != "", KeyHint: v.KeyHint(),
		Model: e.Model, Eagerness: e.Eagerness, Voice: e.Voice, Speed: e.Speed, ExtraInstructions: e.ExtraInstructions,
		Briefing: !e.BriefingOff, Brief: !e.LongAnswers, BriefingAssistant: e.BriefingAssistant, WorkEnabled: e.WorkEnabled, WorkAssistant: e.WorkAssistant,
		Models: copilot.Models, Eagernesses: copilot.Eagerness, Voices: copilot.Voices, Speeds: copilot.Speeds, BriefingAssistants: copilot.BriefingAssistants,
		MaxInstructions: copilot.MaxExtraInstructionsRunes,
	}
}

// copilotSettingsRoute is GET/POST /v1/settings/copilot. A paired device
// needs Send to write it (gate.go); no answer ever carries the key.
func (s *Server) copilotSettingsRoute(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		v, err := s.readCopilot()
		if err != nil {
			writeRefusal(w, http.StatusInternalServerError, "copilot_settings_unreadable", "The voice copilot settings on this machine cannot be read.")
			return
		}
		writeJSON(w, copilotAnswer(v))
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "a voice copilot settings write is application/json")
			return
		}
		var c copilot.Change
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&c); err != nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "That body is not a voice copilot settings change.")
			return
		}
		v, err := s.changeCopilot(c)
		switch {
		case errors.Is(err, copilot.ErrBadKey):
			writeRefusal(w, http.StatusBadRequest, "copilot_bad_key", "An OpenAI API key starts with sk- and has no spaces.")
		case errors.Is(err, copilot.ErrBadModel), errors.Is(err, copilot.ErrBadEagerness), errors.Is(err, copilot.ErrBadVoice), errors.Is(err, copilot.ErrBadSpeed), errors.Is(err, copilot.ErrBadAssistant):
			writeRefusal(w, http.StatusBadRequest, err.Error(), "That choice is not one this machine offers.")
		case errors.Is(err, copilot.ErrBadInstructions):
			writeRefusal(w, http.StatusBadRequest, "copilot_bad_instructions", "The extra instructions are too long.")
		case err != nil:
			writeRefusal(w, http.StatusInternalServerError, "copilot_settings_unwritable", "The voice copilot settings on this machine cannot be saved.")
		default:
			writeJSON(w, copilotAnswer(v))
		}
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use GET or POST.")
	}
}

// copilotSessionAnswer opens one call: a client secret for its model, and
// the person's choices the call's session.update carries.
type copilotSessionAnswer struct {
	WorkEnabled       bool   `json:"work_enabled"`
	ClientSecret      string `json:"client_secret"`
	ExpiresAt         int64  `json:"expires_at"`
	Model             string `json:"model"`
	Eagerness         string `json:"eagerness"`
	Voice             string `json:"voice"`
	Speed             string `json:"speed"`
	ExtraInstructions string `json:"extra_instructions"`
	Briefing          bool   `json:"briefing"`
	Brief             bool   `json:"brief"`
}

// copilotSessionRoute is POST /v1/voice/copilot-session: it mints a
// short-lived Realtime client secret with the machine's key. A paired device
// needs Send (gate.go): the secret starts a call billed to the key.
func (s *Server) copilotSessionRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Use POST.")
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
	secret, expires, status, detail := mintCopilotSecret(ctx, v.Key, e.Model)
	if secret == "" {
		code := "copilot_mint_failed"
		if status == http.StatusUnauthorized {
			code = "copilot_key_rejected"
		}
		writeRawRefusal(w, http.StatusBadGateway, code, "OpenAI did not open a voice copilot call: "+detail)
		return
	}
	writeJSON(w, copilotSessionAnswer{WorkEnabled: e.WorkEnabled, ClientSecret: secret, ExpiresAt: expires, Model: e.Model,
		Eagerness: e.Eagerness, Voice: e.Voice, Speed: e.Speed, ExtraInstructions: e.ExtraInstructions, Briefing: !e.BriefingOff, Brief: !e.LongAnswers})
}

// mintCopilotSecret asks OpenAI for one client secret. The detail it returns
// is OpenAI's own message, which never contains the key.
func mintCopilotSecret(ctx context.Context, key, model string) (string, int64, int, string) {
	body, _ := json.Marshal(map[string]any{
		"expires_after": map[string]any{"anchor": "created_at", "seconds": copilot.ClientSecretSeconds},
		"session":       map[string]any{"type": "realtime", "model": model},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, copilotOpenAI+"/v1/realtime/client_secrets", bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, "the request could not be built"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", 0, 0, "OpenAI did not answer in time"
		}
		return "", 0, 0, "OpenAI could not be reached"
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	var answer struct {
		Value     string `json:"value"`
		ExpiresAt int64  `json:"expires_at"`
		Error     struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &answer)
	if res.StatusCode/100 != 2 || answer.Value == "" {
		detail := strings.TrimSpace(answer.Error.Message)
		if detail == "" {
			detail = http.StatusText(res.StatusCode)
		}
		// OpenAI echoes a mistyped key's ends; say no more than its code then.
		if strings.Contains(detail, "sk-") {
			detail = "incorrect API key (" + answer.Error.Code + ")"
		}
		return "", 0, res.StatusCode, detail
	}
	return answer.Value, answer.ExpiresAt, res.StatusCode, ""
}
