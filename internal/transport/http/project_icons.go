package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/sainteye/clawdline/internal/domain/icon"
)

// projectIconRoute names a place on the receiving machine, never a supplied
// filesystem path. The Cloud bridge reaches this same device-authorized route.
func (s *Server) projectIconRoute(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPut {
		writeRefusal(w, 405, "method_not_allowed", "Copy an icon with PUT.")
		return
	}
	if !maySend(r) {
		writeRefusal(w, 403, "forbidden", "This device may read, and not send.")
		return
	}
	if r.URL.RawQuery != "" {
		writeRefusal(w, 400, "bad_request", "An icon copy takes no query fields.")
		return
	}
	var body struct {
		Icon     icon.Grid `json:"icon"`
		Expected icon.Grid `json:"expected"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, icon.MaxIconRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeRefusal(w, 413, "body_too_large", "The icon copy exceeds 96 KiB.")
		} else {
			writeRefusal(w, 400, "invalid_icon", "Supply an icon and the expected current icon.")
		}
		return
	}
	if dec.Decode(new(any)) != io.EOF {
		writeRefusal(w, 400, "invalid_icon", "Supply one JSON object.")
		return
	}
	for _, grid := range []icon.Grid{body.Icon, body.Expected} {
		if err := icon.Validate(grid); err != nil {
			writeRefusal(w, 422, "invalid_icon", err.Error())
			return
		}
	}
	project, found := s.workV2Project(r.Context(), id)
	if !found {
		writeRefusal(w, 404, "project_not_found", "Choose a Project on this machine.")
		return
	}
	// A mirrored project's mark belongs to its source machine
	// (project_sync.go); change it there, or detach the mirror first.
	if s.icons.Mirrored(project.Path) {
		writeRefusal(w, 409, "project_mirrored", "Another machine owns this Project's settings. Change the icon there, or stop mirroring it here first.")
		return
	}
	err := s.icons.Save(project.Path, body.Icon, body.Expected)
	switch {
	case errors.Is(err, icon.ErrIconChanged):
		writeRefusal(w, 409, "icon_changed", err.Error())
	case errors.Is(err, icon.ErrIconCapacity):
		writeRefusal(w, 409, "icon_capacity", err.Error())
	case err != nil:
		writeRefusal(w, 503, "icon_store_unavailable", "The icon could not be saved. Try again.")
	default:
		writeJSON(w, map[string]any{"ok": true, "icon": wireIcon(s.icons.For(project.Path))})
	}
}
