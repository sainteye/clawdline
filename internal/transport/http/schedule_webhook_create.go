package http

import (
	"errors"
	"net/http"
	"strings"

	adaptercloud "github.com/sainteye/clawdline/internal/adapters/cloud"
)

// Creation is local agent-only. The user's recent run authorizes this standing
// capability; no secret is filed in the daemon's durable HTTP receipt store.
func (s *Server) scheduleWebhookCreateRoute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeAuthRefusal(w, 405, "bad_request", "No such route")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, 403, "forbidden", "That needs the orchestrator token.")
		return
	}
	body := scheduleBody(scheduleRaw(r))
	if len(body) != 2 || body["session_id"] == nil || body["via"] == nil {
		writeAuthRefusal(w, 400, "invalid_user_authorization", "Supply session_id and via.run for the person's instruction.")
		return
	}
	_, refusal := s.scheduleAuthority(r.Context(), true, body)
	if refusal != nil {
		writeScheduleReply(w, *refusal)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		writeAuthRefusal(w, 400, "bad_request", "That needs an Idempotency-Key header.")
		return
	}
	create := s.scheduleBook().CreateWebhook
	if create == nil {
		writeAuthRefusal(w, 409, "cloud_unavailable", "Cloud webhook creation is unavailable on this machine.")
		return
	}
	made, err := create(r.Context(), key)
	if err != nil {
		status, code := 503, "webhook_creation_unavailable"
		var api *adaptercloud.APIError
		if errors.As(err, &api) {
			status, code = api.Status, api.Code
		}
		// Never forward Cloud error text: it can contain credentials.
		writeAuthRefusal(w, status, code, "Cloud did not confirm webhook creation. Retry with the same key; do not create a second webhook.")
		return
	}
	writeJSON(w, made)
}
