package http

import (
	"context"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/task"
)

// obligations publishes everything still owed, loudest first.
//
// This is the route that replaces asking. Every row names who can clear it, how
// long it has been owed and how loud it has become, so "what is stuck and who
// has to move" is read rather than broadcast — the alternative costs one turn
// from every recipient and has already produced seventeen replies all saying
// nothing was wrong.
//
// What is owed is read off the broker's own records — one row per landing
// still pending — and not off a table of its own (D01): the table this used to
// read was written by a dispatch path the broker never took, so a delivery
// waiting to land was never on it.
func (s *Server) obligations(w http.ResponseWriter, r *http.Request) {
	open, err := s.owed(r.Context(), s.reading(r.Context()).Sessions)
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

// owed is what every session on the machine still owes, against one reading of
// the sessions: the broker's pending landings, each named against the terminal
// its root is in now (orchestrator.Owed). The session list, the close action
// and this route all ask it, so a row that reads blocked is the close that
// refuses, for the same reason.
func (s *Server) owed(ctx context.Context, sessions []session.Session) ([]task.Obligation, error) {
	return s.broker.Owed(ctx, sessions)
}
