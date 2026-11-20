package http

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (s *Server) projectMemoryDreamRoute(w http.ResponseWriter, r *http.Request, placeID, restore string) {
	method := http.MethodGet
	if restore != "" {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if r.URL.RawQuery != "" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "")
		return
	}
	if restore != "" && !machineAuthed(r) && !maySend(r) {
		writeRefusal(w, http.StatusForbidden, "forbidden", "")
		return
	}
	project, found := s.workV2Project(r.Context(), placeID)
	if !found {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "")
		return
	}
	key, ok := memory.KeyFor(project.Path)
	if !ok {
		writeRefusal(w, http.StatusNotFound, "project_not_found", "")
		return
	}
	store := s.memoryStore()
	if restore != "" {
		result, err := store.Restore(key, restore)
		if err != nil {
			writeMemoryRefusal(w, err)
			return
		}
		writeJSON(w, contract.ProjectMemoryWriteAnswer{Name: restore, Outcome: contract.ProjectMemoryOutcome(result)})
		return
	}
	plan, err := store.PlanDream(key)
	if err != nil {
		writeMemoryRefusal(w, err)
		return
	}
	writeJSON(w, plan)
}

func (s *Server) maintainProjectMemory(ctx context.Context) {
	ticker := time.NewTicker(memory.DreamQuietTime)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			busy := map[string]bool{}
			if inv, ok := s.readings.Held(); ok {
				for _, current := range inv.Sessions {
					if current.State == session.StateWorking && now.Sub(current.Observation.ObservedAt) <= memory.DreamQuietTime {
						if key, ok := memory.KeyFor(current.CWD); ok {
							busy[key] = true
						}
					}
				}
			}
			err := s.memoryStore().MaintainDreams(ctx, now, func(key string) bool { return busy[key] })
			if err != nil && ctx.Err() == nil {
				log.Printf("project memory maintenance: %v", err)
			}
		}
	}
}
