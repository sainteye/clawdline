package http

import (
	"encoding/json"
	"mime"
	"net/http"
	"runtime"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/adapters/whisper"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// voiceSettingKeys are the only settings /v1/settings/voice reads or writes:
// the LLM cleanup and the personal vocabulary (docs/voice.md).
var voiceSettingKeys = map[string]bool{
	"voice_polish": true, "voice_polish_assistant": true, "voice_polish_level": true,
	"voice_vocabulary": true, "voice_vocabulary_projects": true,
}

// voiceSettingsAnswer is the voice cards' whole state: the five keys as the
// file has them (null when absent), and what this machine offers for them.
type voiceSettingsAnswer struct {
	Polish           *bool    `json:"voice_polish"`
	Assistant        *string  `json:"voice_polish_assistant"`
	Level            *string  `json:"voice_polish_level"`
	Vocabulary       []string `json:"voice_vocabulary"`
	Projects         *bool    `json:"voice_vocabulary_projects"`
	PolishAssistants []string `json:"polish_assistants"`
	Builtin          []string `json:"vocabulary_builtin"`
	ProjectTerms     []string `json:"vocabulary_projects"`
	// Machine is this machine's name as Cloud lists it, so a phone that can
	// switch machines says which one the cards change.
	Machine string `json:"machine"`
}

// voiceSettingsRoute is the narrow settings surface carried to a paired Cloud
// console, so a phone can turn cleanup on and edit the vocabulary. Like
// defaultModelsRoute it does not reuse settingsRoute's answer: a phone
// changing dictation has no reason to receive the hotkey, the remote hostname
// or any other machine setting. The write gate (gate.go) asks a remote device
// for Send.
func (s *Server) voiceSettingsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		v, err := f.Read()
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, s.voiceSettingsSnapshot(f, v))
	case http.MethodPost:
		if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type",
				"a voice settings write is application/json")
			return
		}
		var body map[string]json.RawMessage
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if err := dec.Decode(&body); err != nil || body == nil {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a voice settings change")
			return
		}
		for name := range body {
			if !voiceSettingKeys[name] {
				writeRawRefusal(w, http.StatusBadRequest, "bad_request", "not a voice setting: "+name)
				return
			}
		}
		changes, refusal := settingsChanges(body)
		if refusal != nil {
			writeRawRefusal(w, http.StatusBadRequest, refusal.code, refusal.message)
			return
		}
		var (
			v   nextconfig.Values
			err error
		)
		if len(changes) == 0 {
			v, err = f.Read()
		} else {
			v, err = f.Set(changes)
		}
		if err != nil {
			writeSettingsFailure(w, f, err)
			return
		}
		writeJSON(w, s.voiceSettingsSnapshot(f, v))
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
	}
}

func (s *Server) voiceSettingsSnapshot(f *nextconfig.File, v nextconfig.Values) voiceSettingsAnswer {
	snapshot := settingsSnapshot(f, v)
	out := voiceSettingsAnswer{
		Polish: snapshot.VoicePolish, Assistant: snapshot.VoicePolishAssistant, Level: snapshot.VoicePolishLevel,
		Vocabulary: snapshot.VoiceVocabulary, Projects: snapshot.VoiceVocabularyProjects,
		PolishAssistants: voicePolishAvailable(),
		Builtin:          append([]string{}, whisper.AlwaysExpected...),
		ProjectTerms:     s.voiceProjectTerms(),
	}
	// The name the machine registers and publishes under (cloud_onboarding.go):
	// the settings override, then the host name.
	cloud, _ := adaptercloud.ReadSettings(f)
	out.Machine = cloudtransport.MachineName(cloudtransport.HostName(), runtime.GOOS, "", cloud.MachineName)
	if out.Vocabulary == nil {
		out.Vocabulary = []string{}
	}
	if out.ProjectTerms == nil {
		out.ProjectTerms = []string{}
	}
	return out
}
