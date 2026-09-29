package http

import (
	"net/http"
	"net/url"

	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
)

// projectTreeRoute reads one directory or UTF-8 file beneath a registered Project.
func (s *Server) projectTreeRoute(w http.ResponseWriter, r *http.Request, placeID string, file bool) {
	if r.Method != http.MethodGet {
		writeRefusal(w, 405, "method_not_allowed", "The Project tree is read-only.")
		return
	}
	if r.ContentLength > 0 {
		writeRefusal(w, 400, "bad_request", "A Project tree read takes no body.")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	field := "directory"
	if file {
		field = "path"
	}
	if err != nil || len(query) > 1 || len(query[field]) > 1 ||
		(len(query) == 1 && query[field] == nil) || file && len(query[field]) != 1 {
		writeRefusal(w, 400, "bad_request", "Supply one Project-relative directory or file path.")
		return
	}
	value := query.Get(field)
	if _, err := projectfiles.TreePath(value, file); err != nil {
		writeRefusal(w, 400, "bad_tree_path", "Choose a file or folder within this Project.")
		return
	}
	project, found := s.workV2Project(r.Context(), placeID)
	if !found {
		writeRefusal(w, 404, "project_not_found", "Choose a Project that this machine currently lists.")
		return
	}
	if file {
		content, err := projectfiles.ReadTree(project.Path, value)
		if err != nil {
			writeProjectFileRefusal(w, err)
			return
		}
		writeJSON(w, content)
		return
	}
	listing, err := projectfiles.ListTree(project.Path, value)
	if err != nil {
		writeProjectFileRefusal(w, err)
		return
	}
	writeJSON(w, listing)
}
