package http

import (
	"net/http"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/orchestrator"
	"github.com/sainteye/clawdline-go/internal/domain/work"
)

// `GET /v1/orchestrator/sessions/{session_id}/todos?state=&cursor=`: what one
// session owes (board-redesign §5.2 (3), design-decisions T2).
//
// The reader is the session — its root, whoever takes it over — through the
// machine token, the same credential that dispatched the work. It is not a
// person's page: nothing that draws the board reads this, and nothing here is
// pushed anywhere (#5, "不應該和給人看的項目混在一起"). The list is read, never
// written, through this route; every change to it is the broker's, from a
// fact.

var todosQueryKeys = map[string]bool{"state": true, "cursor": true}

type todoWire struct {
	ID             string        `json:"id"`
	Origin         string        `json:"origin"`
	TaskID         string        `json:"task_id"`
	WorkID         *string       `json:"work_id"`
	Title          string        `json:"title"`
	Project        string        `json:"project"`
	OwnerSession   string        `json:"owner_session"`
	OwnerAssistant string        `json:"owner_assistant"`
	State          string        `json:"state"`
	Reason         string        `json:"reason"`
	HandedTo       *string       `json:"handed_to"`
	CreatedAt      int64         `json:"created_at"`
	UpdatedAt      int64         `json:"updated_at"`
	HandedOffAt    *int64        `json:"handed_off_at"`
	ClosedAt       *int64        `json:"closed_at"`
	Escalation     []work.Signal `json:"escalation"`
}

type todosWire struct {
	OK         bool           `json:"ok"`
	SessionID  string         `json:"session_id"`
	State      string         `json:"state"`
	Counts     map[string]int `json:"counts"`
	Todos      []todoWire     `json:"todos"`
	NextCursor *string        `json:"next_cursor"`
	PageSize   int            `json:"page_size"`
}

func (s *Server) sessionTodos(w http.ResponseWriter, r *http.Request, session string) {
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "A session's to-do list needs the orchestrator token.")
		return
	}
	q := r.URL.Query()
	for key := range q {
		if !todosQueryKeys[key] {
			writeRefusal(w, http.StatusBadRequest, "bad_request", "unknown query parameter "+key)
			return
		}
	}
	page, err := s.broker.SessionTodos(r.Context(), session, orchestrator.TodoFilter(q.Get("state")), q.Get("cursor"))
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	out := todosWire{OK: true, SessionID: page.Session, State: string(page.Filter), Counts: map[string]int{},
		Todos: make([]todoWire, 0, len(page.Todos)), PageSize: page.PageSize}
	for _, st := range work.TodoStates {
		out.Counts[string(st)] = page.Counts[st]
	}
	for _, t := range page.Todos {
		out.Todos = append(out.Todos, todoWire{
			ID: t.ID, Origin: string(t.Origin), TaskID: t.Task, WorkID: optionalString(t.WorkID),
			Title: t.Title, Project: t.Project, OwnerSession: t.Owner, OwnerAssistant: t.OwnerAssistant,
			State: string(t.State), Reason: t.Reason, HandedTo: optionalString(t.HandedTo),
			CreatedAt: t.CreatedAt.Unix(), UpdatedAt: t.UpdatedAt.Unix(),
			HandedOffAt: optionalUnix(t.HandedOffAt), ClosedAt: optionalUnix(t.ClosedAt),
			Escalation: t.Escalation,
		})
	}
	if page.Next != "" {
		out.NextCursor = &page.Next
	}
	writeJSON(w, out)
}

func optionalString(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func optionalUnix(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	v := t.Unix()
	return &v
}
