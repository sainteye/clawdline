package http

import (
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
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
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "store_unreadable", err.Error())
		return
	}

	now := time.Now()
	rows := make([]contract.Obligation, 0, len(open))
	for _, o := range open {
		rows = append(rows, contract.Obligation{
			ID:         o.ID,
			Kind:       contract.ObligationKind(o.Kind),
			Subject:    o.Subject,
			Mover:      wireMover(o.Mover),
			OpenedAt:   o.OpenedAt.Unix(),
			AgeSeconds: int64(o.Age(now).Seconds()),
			Escalation: contract.Escalation(o.Escalate(now, task.DefaultThresholds)),
			Evidence:   contract.Evidence(o.Evidence),
			Note:       o.Note,
		})
	}
	writeJSON(w, contract.ObligationList{
		Obligations: rows,
		Stuck:       int64(len(task.Stuck(open, now, task.DefaultThresholds))),
		At:          now.Unix(),
	})
}
