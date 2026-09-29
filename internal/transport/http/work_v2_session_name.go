package http

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
)

// agentNameItemSession lets a Root name only the new Session opened for its
// current Board assignment. The naming choice comes from the Root's existing
// reasoning turn; this route starts no model or other external call.
func (s *Server) agentNameItemSession(w http.ResponseWriter, r *http.Request, itemID string) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, workV2BodyLimit))
	if err != nil {
		writeRefusal(w, http.StatusRequestEntityTooLarge, "body_too_large", "A work-system request is at most 96 KiB.")
		return
	}
	if !utf8.Valid(raw) {
		writeRefusal(w, http.StatusBadRequest, "invalid_title", "The Session name must be valid UTF-8.")
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Title     string `json:"title"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "invalid_request", "The body is not a valid naming request.")
		return
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		writeRefusal(w, http.StatusBadRequest, "invalid_request", "A naming request contains one JSON object.")
		return
	}
	title := normalizedSessionTitle(body.Title)
	if title == "" || !titleWithinLimit(title) || len(title) > orchestrator.AssignmentLabelLimit() {
		writeRefusal(w, http.StatusUnprocessableEntity, "invalid_title", "The Session name must be one nonempty line of at most 200 characters and 200 UTF-8 bytes.")
		return
	}
	if strings.TrimSpace(body.SessionID) == "" {
		writeRefusal(w, http.StatusBadRequest, "session_required", "Name the owning Session's conversation id.")
		return
	}
	named, err := s.workV2().NameOwnedSession(r.Context(), itemID, body.SessionID, title)
	if err != nil {
		s.writeWorkV2Error(w, err)
		return
	}
	var display *string
	if live, err := s.broker.LiveRootSession(r.Context(), body.SessionID); err == nil {
		shown := s.sessionDisplayLabel(r.Context(), live)
		display = &shown
	}
	writeJSON(w, map[string]any{"ok": true, "stored_title": named.Label, "display_title": display})
}
