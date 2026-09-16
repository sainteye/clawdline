package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/adapters/store"
	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/board"
)

// boardRead publishes the board: its revision, and the projects derived from
// what is running on this machine right now.
func (s *Server) boardRead(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	revision, err := s.store.BoardRevision(ctx)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}

	inv := s.inventory.Read(ctx)
	cwds := make([]string, 0, len(inv.Sessions))
	for _, item := range inv.Sessions {
		if item.IsAssistant() {
			cwds = append(cwds, item.CWD)
		}
	}

	derived := board.DeriveProjects(cwds)
	projects := make([]contract.Project, 0, len(derived))
	for _, p := range derived {
		projects = append(projects, contract.Project{
			ID:           p.ID,
			Name:         p.Name,
			DisplayPath:  p.DisplayPath,
			SessionCount: int64(p.SessionCount),
		})
	}

	writeJSON(w, contract.BoardSnapshot{
		SchemaVersion: 1,
		Revision:      revision,
		Projects:      projects,
		// The reading this rests on says whether it was complete. A board built
		// from a partial inventory is not a board with fewer projects, it is a
		// board that does not know.
		Source: contract.BoardSource{
			Complete:   inv.Complete,
			Provenance: inv.Provenance,
			ObservedAt: inv.ObservedAt.Unix(),
		},
		At: time.Now().Unix(),
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
		writeJSON(w, contract.BoardWriteResult{OK: true, Revision: revision, Replayed: true})
		return
	}

	next, err := s.store.ApplyBoardCommand(ctx, c.RequestID, c.Fingerprint(),
		[]store.Event{{Kind: "board." + c.Operation, Subject: c.Actor, Payload: c.Body}})
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unwritable", err.Error())
		return
	}
	writeJSON(w, contract.BoardWriteResult{OK: true, Revision: next, Replayed: false})
}
