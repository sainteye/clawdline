package http

import (
	"encoding/json"
	"net/http"
)

// tasksRoute publishes the tasks this daemon knows about.
func (s *Server) tasksRoute(w http.ResponseWriter, r *http.Request) {
	live, err := s.store.LiveTasks(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	rows := make([]map[string]any, 0, len(live))
	for _, t := range live {
		rows = append(rows, map[string]any{
			"task_id":     t.ID,
			"assistant":   string(t.Assistant),
			"project_dir": t.ProjectDir,
			"claims":      t.Claims,
			"state":       string(t.State),
			"created_at":  t.CreatedAt.Unix(),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": rows})
}

// strings answers with the localisation catalog this daemon carries.
//
// It carries none yet. An empty catalog is the honest answer: the console has
// built-in English and will use it, which is a visible, explainable degradation
// rather than a missing route that looks like a fault. Porting the catalog is
// its own piece of work, and it is named in docs/plan.md rather than faked
// here.
func (s *Server) strings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{})
}
