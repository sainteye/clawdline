package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/domain/board"
)

// boardRead publishes the board: its revision, and the projects derived from
// what is running on this machine right now.
func (s *Server) boardRead(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	revision, err := s.store.BoardRevision(ctx)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "store_unreadable", "detail": err.Error()})
		return
	}

	inv := s.inventory.Read(ctx)
	cwds := make([]string, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if item.IsAssistant() {
			cwds = append(cwds, item.CWD)
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"schemaVersion": 1,
		"revision":      revision,
		"projects":      board.DeriveProjects(cwds),
		// The reading this rests on says whether it was complete. A board built
		// from a partial inventory is not a board with fewer projects, it is a
		// board that does not know.
		"source": map[string]any{
			"complete":   inv.Complete,
			"provenance": inv.Provenance,
			"observedAt": inv.ObservedAt.Unix(),
		},
		"at": time.Now().Unix(),
	})
}

// boardWrite applies one command under compare-and-swap.
func (s *Server) boardWrite(w http.ResponseWriter, r *http.Request) {
	var c board.Command
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a command")
		return
	}
	ctx := r.Context()
	revision, err := s.store.BoardRevision(ctx)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	seen, err := s.store.BoardSeen(ctx)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}

	apply, decideErr := board.Decide(revision, seen, c)
	if decideErr != nil {
		refusal, ok := decideErr.(board.Refusal)
		if !ok {
			writeRefusal(w, http.StatusInternalServerError, "internal", decideErr.Error())
			return
		}
		status := http.StatusConflict
		if refusal.Code == "bad_request" {
			status = http.StatusBadRequest
		}
		writeRefusal(w, status, refusal.Code, refusal.Detail)
		return
	}
	if !apply {
		// A replay. The original outcome is the answer; nothing moves.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "revision": revision, "replayed": true,
		})
		return
	}

	next, err := s.store.ApplyBoardCommand(ctx, c.RequestID, c.Fingerprint(),
		[]store.Event{{Kind: "board." + c.Operation, Subject: c.Actor, Payload: c.Body}})
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unwritable", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "revision": next, "replayed": false,
	})
}

func writeRefusal(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": code, "detail": detail})
}
