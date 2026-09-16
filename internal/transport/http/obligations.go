package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// obligations publishes everything still owed, loudest first.
//
// This is the route that replaces asking. Every row names who can clear it, how
// long it has been owed and how loud it has become, so "what is stuck and who
// has to move" is read rather than broadcast — the alternative costs one turn
// from every recipient and has already produced seventeen replies all saying
// nothing was wrong.
func (s *Server) obligations(w http.ResponseWriter, r *http.Request) {
	open, err := s.store.OpenObligations(r.Context())
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "store_unreadable", "detail": err.Error()})
		return
	}

	now := time.Now()
	rows := make([]map[string]any, 0, len(open))
	for _, o := range open {
		rows = append(rows, map[string]any{
			"id":          o.ID,
			"kind":        string(o.Kind),
			"subject":     o.Subject,
			"mover":       map[string]any{"kind": string(o.Mover.Kind), "id": o.Mover.ID},
			"opened_at":   o.OpenedAt.Unix(),
			"age_seconds": int(o.Age(now).Seconds()),
			"escalation":  string(o.Escalate(now, task.DefaultThresholds)),
			"evidence":    string(o.Evidence),
			"note":        o.Note,
		})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"obligations": rows,
		"stuck":       len(task.Stuck(open, now, task.DefaultThresholds)),
		"at":          now.Unix(),
	})
}
