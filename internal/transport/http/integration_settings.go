package http

import (
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"io"
	"mime"
	"net/http"
)

// integrationSettingsRoute exposes only machine-wide opt-ins. A partial write
// cannot modify another integration or unrelated machine settings.
func (s *Server) integrationSettingsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	read := func() (map[string]any, error) {
		values, err := f.Read()
		if err != nil {
			return nil, err
		}
		profile, err := nextconfig.WorkflowProfile(values)
		if err != nil {
			return nil, err
		}
		graphify, _ := values.Bool("graphify_enabled")
		return map[string]any{"workflow_profile": profile, "workflow_skills_enabled": profile == "superpowers", "graphify_enabled": graphify}, nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
	case http.MethodPost:
		if contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || contentType != "application/json" {
			writeRefusal(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "An integration settings write is application/json.")
			return
		}
		var body map[string]json.RawMessage
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, settingsRequestBodyLimit))
		if decoder.Decode(&body) != nil || body == nil || decoder.Decode(new(any)) != io.EOF {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "That body is not an integration settings change.")
			return
		}
		changes := make(map[string]any, len(body))
		for name, raw := range body {
			if name != "workflow_skills_enabled" && name != "graphify_enabled" && name != "workflow_profile" {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "Not an integration setting key: "+name)
				return
			}
			var value any
			if json.Unmarshal(raw, &value) != nil {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "An integration setting must be a boolean.")
				return
			}
			if name == "workflow_profile" {
				id, ok := value.(string)
				if !ok || (id != "clawdline" && id != "superpowers" && id != "poteto") {
					writeRefusal(w, http.StatusBadRequest, "invalid_workflow_profile", "Choose clawdline, superpowers or poteto.")
					return
				}
				changes[name] = id
				continue
			}
			on, ok := value.(bool)
			if !ok {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "An integration setting must be a boolean.")
				return
			}
			changes[name] = on
		}
		if _, profile := changes["workflow_profile"]; profile {
			if _, legacy := changes["workflow_skills_enabled"]; legacy {
				writeRefusal(w, http.StatusBadRequest, "conflicting_workflow_settings", "Send either workflow_profile or the legacy opt-in, not both.")
				return
			}
			changes["workflow_skills_enabled"] = changes["workflow_profile"] == "superpowers"
		} else if legacy, ok := changes["workflow_skills_enabled"].(bool); ok {
			changes["workflow_profile"] = "clawdline"
			if legacy {
				changes["workflow_profile"] = "superpowers"
			}
		}
		if len(changes) != 0 {
			if _, err := f.Set(changes); err != nil {
				writeSettingsFailure(w, f, err)
				return
			}
		}
	default:
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET or POST")
		return
	}
	answer, err := read()
	if err != nil {
		writeSettingsFailure(w, f, err)
		return
	}
	writeJSON(w, answer)
}
