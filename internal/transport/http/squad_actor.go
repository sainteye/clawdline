package http

import (
	"net/http"
)

// requireSquadWorkActor makes a Board agent mutation prove the active root
// Session that owns the Epic. Legacy sessions without a snapshot retain their
// prior machine-token path; a bound launch never gets that fallback.
func (s *Server) requireSquadWorkActor(w http.ResponseWriter, r *http.Request, conversationID string) bool {
	capability := r.Header.Get(squadSessionCapabilityHeader)
	actor, bound, err := s.store.AuthenticateSquadActor(r.Context(), capability)
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The Session actor could not be checked.")
		return false
	}
	if bound && actor.ConversationID == conversationID {
		return true
	}
	if !bound && capability == "" {
		_, _, hasSnapshot, err := s.store.SquadSnapshotForConversation(r.Context(), conversationID)
		if err != nil {
			writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The Session actor could not be checked.")
			return false
		}
		if !hasSnapshot {
			return true
		}
	}
	writeRefusal(w, http.StatusForbidden, "session_actor_required", "The current owner Session's squad capability is required.")
	return false
}
