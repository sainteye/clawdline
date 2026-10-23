package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/antigravitysession"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/contract"
)

// Owned history uses the same project and pinned-machine routes as other
// providers. Saved pages are offline; explicit native pages use metadata only.
func (s *Server) pastOwnedAntigravity(w http.ResponseWriter, r *http.Request, ctx context.Context, place projects.Place, limit int) {
	h := s.antigravitySessions.Load()
	if h == nil {
		refuseAntigravityFoundation(w)
		return
	}
	cursor := r.URL.Query().Get("cursor")
	var rows []antigravitysession.Past
	var next string
	var err error
	if !strings.HasPrefix(cursor, "native:") {
		rows, next, err = h.Past(ctx, place.Path, cursor, limit)
	}
	if strings.HasPrefix(cursor, "native:") || (err == nil && len(rows) == 0 && cursor == "") {
		rows, next, err = h.NativePast(ctx, place.Path, strings.TrimPrefix(cursor, "native:"))
		if next != "" {
			next = "native:" + next
		}
	} else if err == nil && next == "" {
		next = "native:"
	}
	if err != nil {
		writePlaceRefusal(w, http.StatusServiceUnavailable, "provider_history_unavailable", err.Error(), "Gemini")
		return
	}
	out := contract.PastSessionList{At: time.Now().Unix(), Place: place.ID, Assistant: "antigravity", More: next != "", NextCursor: next, Sessions: []contract.PastSession{}}
	for _, row := range rows {
		at := int64(0)
		if !row.At.IsZero() {
			at = row.At.Unix()
		}
		out.Sessions = append(out.Sessions, contract.PastSession{ID: row.ID, Title: row.Title, At: at, Live: row.Live})
	}
	writeJSON(w, out)
}

func (s *Server) resumeOwnedAntigravity(w http.ResponseWriter, r *http.Request, id, conversation, persona string) {
	h := s.antigravitySessions.Load()
	if h == nil {
		refuseAntigravityFoundation(w)
		return
	}
	if persona != "" {
		writePlaceRefusal(w, http.StatusConflict, "provider_operation_unsupported", "The personal ACP preview currently uses its native default model and no role.", "Gemini")
		return
	}
	release, ok := admitOpening(w, r)
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	place, ok := s.starter(s.readForStart(ctx)).Place(ctx, id)
	if !ok {
		writePlaceRefusal(w, http.StatusNotFound, "not_found", "No place named that", "")
		return
	}
	row, err := h.Import(ctx, place.Path, conversation)
	if err != nil {
		writePlaceRefusal(w, http.StatusConflict, "provider_resume_failed", err.Error(), "Gemini")
		return
	}
	writeJSON(w, contract.PlaceResumed{OK: true, ID: row.ID, Backend: contract.BackendOwned, Assistant: "antigravity", Place: place.ID, CWD: place.Path, Session: conversation, At: time.Now().Unix()})
}
