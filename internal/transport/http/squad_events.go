package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/sainteye/clawdline/internal/adapters/store"
)

const maxSquadEventBodyBytes = 4 << 10

const squadSessionCapabilityHeader = "X-Clawdline-Session-Capability"

type squadSessionBindingWire struct {
	SessionID      string `json:"session_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	State          string `json:"state"`
	SnapshotID     string `json:"snapshot_id,omitempty"`
	DefinitionID   string `json:"definition_id,omitempty"`
	ScopeID        string `json:"scope_id,omitempty"`
}

// squadSessionBindings joins current terminal and provider identities with
// the persisted launch binding. An unsnapshotted legacy session is explicit;
// the caller never has to guess a Project from cwd or a persona from argv.
func (s *Server) squadSessionBindings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Session bindings are read with GET.")
		return
	}
	reading := s.reading(r.Context())
	bindings := make([]squadSessionBindingWire, 0, len(reading.Sessions))
	for _, item := range reading.Sessions {
		if !item.IsAssistant() {
			continue
		}
		row := squadSessionBindingWire{SessionID: item.ID, ConversationID: item.ConversationID}
		if item.ConversationID == "" {
			row.State = "identity_unavailable"
		} else {
			binding, found, err := s.store.SquadBindingForSession(r.Context(), item.ID, item.ConversationID)
			if err != nil {
				writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "Session bindings could not be read.")
				return
			}
			if !found {
				row.State = "legacy_unsnapshotted"
			} else {
				row.State = "bound"
				row.SnapshotID, row.DefinitionID, row.ScopeID = binding.SnapshotID, binding.DefinitionID, binding.ScopeID
			}
		}
		bindings = append(bindings, row)
	}
	writeJSON(w, struct {
		Bindings []squadSessionBindingWire `json:"bindings"`
	}{Bindings: bindings})
}

func (s *Server) squadEventHead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "The event head is read with GET.")
		return
	}
	head, err := s.store.SquadEventHead(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The event head could not be read.")
		return
	}
	writeJSON(w, map[string]int64{"seq": head})
}

func (s *Server) squadEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "Skill events are read with GET.")
		return
	}
	after := int64(0)
	if raw := r.URL.Query().Get("after"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeRefusal(w, http.StatusBadRequest, "invalid_cursor", "The event cursor must be a nonnegative sequence.")
			return
		}
		after = parsed
	}
	pageRows := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > store.MaxSquadEventPageRows {
			writeRefusal(w, http.StatusBadRequest, "invalid_page_size", "The event page size is outside the supported range.")
			return
		}
		pageRows = parsed
	}
	events, more, err := s.store.SquadEventsAfter(r.Context(), after, pageRows)
	if err != nil {
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "Skill events could not be read.")
		return
	}
	if events == nil {
		events = []store.SquadSkillReceipt{}
	}
	next := after
	if len(events) > 0 {
		next = events[len(events)-1].Seq
	}
	writeJSON(w, struct {
		Events    []store.SquadSkillReceipt `json:"events"`
		NextAfter int64                     `json:"next_after"`
		HasMore   bool                      `json:"has_more"`
	}{Events: events, NextAfter: next, HasMore: more})
}

func (s *Server) squadSessionEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A session reports skill use with POST.")
		return
	}
	// A paired Cloud device has a machine-wide read scope, but it is never an
	// agent process. The local token alone also cannot select a session: the
	// bound capability is required and verified by the store below.
	if !accessOf(r).verdict.Local || judgedAsDevice(r) {
		writeRefusal(w, http.StatusForbidden, "session_actor_required", "A local bound session must report this event.")
		return
	}
	capability := r.Header.Get(squadSessionCapabilityHeader)
	var event store.SquadSkillEvent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSquadEventBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		writeRefusal(w, http.StatusBadRequest, "invalid_event", "The skill event body is invalid.")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		writeRefusal(w, http.StatusBadRequest, "invalid_event", "The skill event body has trailing content.")
		return
	}
	receipt, err := s.store.RecordSquadSkillEvent(r.Context(), capability, event)
	switch {
	case errors.Is(err, store.ErrSquadActorUnauthorized):
		writeRefusal(w, http.StatusForbidden, "session_actor_required", "The session capability is missing or not bound.")
	case errors.Is(err, store.ErrSquadEventInvalid):
		writeRefusal(w, http.StatusBadRequest, "invalid_event", "The event does not match an enabled skill in this session's snapshot.")
	case errors.Is(err, store.ErrSquadEventConflict):
		writeRefusal(w, http.StatusConflict, "event_conflict", "This event ID was already used with different content.")
	case err != nil:
		writeRefusal(w, http.StatusServiceUnavailable, "store_unavailable", "The skill event could not be recorded.")
	default:
		writeJSON(w, receipt)
	}
}
