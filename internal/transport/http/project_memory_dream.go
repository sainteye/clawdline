package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/memory"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
)

func (s *Server) projectMemoryDreamRoute(w http.ResponseWriter, r *http.Request, placeID, name, action string) {
	method := http.MethodGet
	if name != "" {
		method = http.MethodPost
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		return
	}
	if (action != "memory-search" && r.URL.RawQuery != "") || action != "review" && (r.ContentLength != 0 || len(r.TransferEncoding) != 0) {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "")
		return
	}
	if name != "" && !machineAuthed(r) && !maySend(r) {
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
	if action == "memory-search" {
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || len(query) != 1 || len(query["query"]) != 1 {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "")
			return
		}
		matches, err := store.Search(key, query.Get("query"))
		if err != nil {
			writeMemoryRefusal(w, err)
			return
		}
		writeJSON(w, struct {
			Matches []string `json:"matches"`
		}{matches})
		return
	}
	if name != "" {
		var result memory.Outcome
		var err error
		switch action {
		case "restore":
			result, err = store.Restore(key, name)
		case "review":
			body, readErr := io.ReadAll(io.LimitReader(r.Body, memoryRequestBodyLimit+1))
			if readErr != nil || len(body) > memoryRequestBodyLimit {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "")
				return
			}
			var review memory.ReviewRequest
			decoder := json.NewDecoder(bytes.NewReader(body))
			decoder.DisallowUnknownFields()
			if decoder.Decode(&review) != nil || decoder.Decode(new(any)) != io.EOF {
				writeRefusal(w, http.StatusBadRequest, "bad_request", "")
				return
			}
			result, err = store.Review(key, name, review)
		}
		if errors.Is(err, memory.ErrReviewStale) {
			writeRefusal(w, http.StatusConflict, "memory_review_stale", "")
			return
		}
		if err != nil {
			writeMemoryRefusal(w, err)
			return
		}
		writeJSON(w, contract.ProjectMemoryWriteAnswer{Name: name, Outcome: contract.ProjectMemoryOutcome(result)})
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
