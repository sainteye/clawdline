package http

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
)

// integrationSettingsRoute exposes only machine-wide opt-ins. A partial write
// cannot modify another integration or unrelated machine settings.
func (s *Server) integrationSettingsRoute(w http.ResponseWriter, r *http.Request) {
	f := s.settingsFile()
	read := func() (map[string]bool, error) {
		values, err := f.Read()
		if err != nil {
			return nil, err
		}
		skills, _ := values.Bool("workflow_skills_enabled")
		graphify, _ := values.Bool("graphify_enabled")
		return map[string]bool{"workflow_skills_enabled": skills, "graphify_enabled": graphify}, nil
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
			if name != "workflow_skills_enabled" && name != "graphify_enabled" {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "Not an integration setting key: "+name)
				return
			}
			var value any
			if json.Unmarshal(raw, &value) != nil {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "An integration setting must be a boolean.")
				return
			}
			on, ok := value.(bool)
			if !ok {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "An integration setting must be a boolean.")
				return
			}
			changes[name] = on
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
