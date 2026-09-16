package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/coordinator"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// coordinatorRoute reads or moves the machine role.
func (s *Server) coordinatorRoute(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	existing, err := s.store.Coordinator(ctx)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "coordinator_store_invalid", err.Error())
		return
	}

	if r.Method != http.MethodPost {
		live, sessions := s.liveness(ctx, existing)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"registered": existing != nil,
			"record":     existing,
			"liveness":   string(live),
			"candidates": sessions,
		})
		return
	}

	var body struct {
		Operation      string `json:"operation"`
		Label          string `json:"label"`
		SessionID      string `json:"sessionId"`
		ConversationID string `json:"conversationId"`
		Assistant      string `json:"assistant"`
		PID            int    `json:"pid"`
		ExpectID       string `json:"expectId"`
		ExpectGen      int64  `json:"expectGeneration"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a coordinator command")
		return
	}

	live, candidates := s.liveness(ctx, existing)

	var next coordinator.Record
	switch body.Operation {
	case "register":
		next, err = coordinator.Register(existing, coordinator.Record{
			ID:             body.SessionID + ":" + body.ConversationID,
			Label:          body.Label,
			SessionID:      body.SessionID,
			ConversationID: body.ConversationID,
			Assistant:      body.Assistant,
			PID:            body.PID,
		}, live)
	case "rebind":
		next, err = coordinator.Rebind(existing, body.ExpectID, body.ExpectGen, candidates, live)
	default:
		writeRefusal(w, http.StatusBadRequest, "bad_request", "operation must be register or rebind")
		return
	}
	if err != nil {
		refusal, ok := err.(coordinator.Refusal)
		if !ok {
			writeRefusal(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		status := http.StatusConflict
		if refusal.Code == "bad_request" {
			status = http.StatusBadRequest
		}
		writeRefusal(w, status, refusal.Code, refusal.Detail)
		return
	}
	if err := s.store.SaveCoordinator(ctx, next); err != nil {
		writeRefusal(w, http.StatusInternalServerError, "coordinator_store_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "record": next})
}

// liveness asks the machine whether the bound session is still there.
//
// An incomplete inventory answers `unknown` rather than `offline`. That is the
// whole rule: a reading that could not see everything has not proved anything
// absent.
func (s *Server) liveness(ctx context.Context, rec *coordinator.Record) (coordinator.Liveness, []string) {
	inv := s.inventory.Read(ctx)
	ids := []string{}
	for _, item := range inv.Sessions {
		if item.IsAssistant() {
			ids = append(ids, item.ID)
		}
	}
	if rec == nil {
		if !inv.Complete {
			return coordinator.Unknown, ids
		}
		return coordinator.Offline, ids
	}
	for _, item := range inv.Sessions {
		if item.ID == rec.SessionID {
			return coordinator.Online, ids
		}
	}
	if !inv.Complete {
		return coordinator.Unknown, ids
	}
	return coordinator.Offline, ids
}

var _ = session.Session{}
