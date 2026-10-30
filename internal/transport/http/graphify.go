package http

import (
	"io"
	"net/http"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/graphify"
)

func sessionSubresourcePath(r *http.Request, name string) (string, bool) {
	parts := strings.Split(strings.TrimPrefix(routePath(r), "/v1/sessions/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != name {
		return "", false
	}
	return decodeSegment(parts[0]), true
}

// The machine switch is checked before resolving a directory or reading an index.
func (s *Server) graphifyAdmission(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		writeRefusal(w, 405, "method_not_allowed", "GET only")
		return false
	}
	if r.Body != nil && r.Body != http.NoBody {
		probe, err := io.ReadAll(io.LimitReader(r.Body, 1))
		if err != nil || len(probe) > 0 {
			writeRefusal(w, 400, "bad_request", "A Graphify view takes no query or body.")
			return false
		}
	}
	if r.URL.RawQuery != "" || r.ContentLength > 0 {
		writeRefusal(w, 400, "bad_request", "A Graphify view takes no query or body.")
		return false
	}
	values, err := s.settingsFile().Read()
	if err != nil {
		writeSettingsFailure(w, s.settingsFile(), err)
		return false
	}
	enabled, _ := values.Bool("graphify_enabled")
	if !enabled {
		writeRefusal(w, 403, "graphify_disabled", "Enable Graphify in this machine's integration settings to view a graph.")
		return false
	}
	return true
}

func (s *Server) projectGraphifyRoute(w http.ResponseWriter, r *http.Request, place string) {
	if !s.graphifyAdmission(w, r) {
		return
	}
	project, found := s.workV2Project(r.Context(), place)
	if !found {
		writeRefusal(w, 404, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	s.writeGraphifyView(w, r, project.Path)
}

func (s *Server) sessionGraphifyRoute(w http.ResponseWriter, r *http.Request, id string) {
	if !s.graphifyAdmission(w, r) {
		return
	}
	item, err := s.actions().FindForRead(r.Context(), id)
	if err != nil {
		writeActionRefusal(w, err)
		return
	}
	if item.CWD == "" {
		writeRefusal(w, 404, "graphify_directory_missing", "This Session has no local working directory.")
		return
	}
	if !s.admitPinnedTarget(w, r, id) {
		return
	}
	s.writeGraphifyView(w, r, item.CWD)
}

func (s *Server) writeGraphifyView(w http.ResponseWriter, r *http.Request, root string) {
	view, err := graphify.View(r.Context(), root)
	if err != nil {
		writeRefusal(w, 503, "graphify_unavailable", "The graph could not be checked. Try again.")
		return
	}
	// Recheck after scanning, so an off switch during the read does not publish data.
	values, err := s.settingsFile().Read()
	enabled, _ := values.Bool("graphify_enabled")
	if err != nil || !enabled {
		writeRefusal(w, 403, "graphify_disabled", "Graphify is disabled or its setting is unavailable.")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, view)
}
