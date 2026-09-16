package http

import (
	"net/http"

	"github.com/sainteye/clawdline-go/internal/contract"
)

// tasksRoute publishes the tasks this daemon knows about.
func (s *Server) tasksRoute(w http.ResponseWriter, r *http.Request) {
	live, err := s.store.LiveTasks(r.Context())
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}
	rows := make([]contract.TaskRow, 0, len(live))
	for _, t := range live {
		rows = append(rows, contract.TaskRow{
			TaskID:     t.ID,
			Assistant:  contract.Assistant(t.Assistant),
			ProjectDir: t.ProjectDir,
			Claims:     t.Claims,
			State:      contract.TaskState(t.State),
			CreatedAt:  t.CreatedAt.Unix(),
		})
	}
	writeJSON(w, contract.TaskList{Tasks: rows})
}

// strings answers with the localisation catalog this daemon carries.
//
// It carries none yet. An empty catalog is the honest answer: the console has
// built-in English and will use it, which is a visible, explainable degradation
// rather than a missing route that looks like a fault. Porting the catalog is
// its own piece of work, and it is named in docs/plan.md rather than faked
// here.
//
// This is the one route with no generated type, because its keys are the
// catalog's own and a schema listing them would be the catalog.
func (s *Server) strings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{})
}
