package http

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/sainteye/clawdline-go/internal/contract"
	"github.com/sainteye/clawdline-go/internal/domain/task"
)

// tasksRoute reads the tasks this daemon knows about, or accepts a new one.
func (s *Server) tasksRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// The broker owns dispatch now (orchestrator.go). The older handler
		// below stays as `s.dispatch` because the scheduler still uses the
		// application command it wraps; nothing routes to it.
		s.brokerDispatch(w, r)
		return
	}
	s.tasksList(w, r)
}

// dispatch accepts one task and starts it.
//
// Two things are decided before anything is created, and both are refusals a
// caller can act on. `claims` absent is not read as none: an undeclared
// dispatch cannot be arbitrated against another root, and the arbitration is
// the point. Overlapping claims are refused by naming the task that holds them
// and how long it has held them, because "busy" alone sends a person looking.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	// A paired device must never be able to start a session: through a tunnel
	// every request comes from 127.0.0.1, so only the orchestrator's own 0600
	// credential opens this.
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Dispatching needs the orchestrator token.")
		return
	}
	var body contract.DispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request", "that body is not a dispatch")
		return
	}

	id := body.TaskID
	if id == "" {
		id = newID()
	}
	t := task.Task{
		ID:         id,
		Assistant:  task.Assistant(body.Assistant),
		ProjectDir: body.ProjectDir,
		Brief:      body.Instructions,
		Claims:     body.Claims,
	}

	// Opening a session and typing into it is slower than a request should
	// wait on a shared timeout, so this one is its own.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 60*time.Second)
	defer cancel()

	receipt, dir, err := s.dispatcher.Dispatch(ctx, t, string(t.Assistant))
	if err != nil {
		writeDispatchRefusal(w, err)
		return
	}
	writeJSON(w, contract.DispatchResult{
		OK:       true,
		TaskID:   t.ID,
		TaskDir:  dir,
		Replayed: receipt.Replayed,
	})
}

// settleRoute asks whether a task has written its result.
func (s *Server) settleRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(routePath(r), "/v1/orchestrator/tasks/")
	id := decodeSegment(strings.TrimSuffix(rest, "/settle"))
	if id == "" || id == rest {
		writeRefusal(w, http.StatusNotFound, "not_found", "that is not a task action")
		return
	}
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"settling records a fact, so it is a POST")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Settling a task needs the orchestrator token.")
		return
	}
	state, settled, err := s.dispatcher.Settle(r.Context(), id)
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "settle_failed", err.Error())
		return
	}
	// Not settled is not an error. It is the ordinary state of work that is
	// still being done, and answering 404 would make "unfinished" look like
	// "never existed".
	writeJSON(w, contract.SettleResult{
		OK:      true,
		TaskID:  id,
		Settled: settled,
		State:   contract.TaskState(state),
	})
}

// writeDispatchRefusal gives each typed refusal the status that fits it.
func writeDispatchRefusal(w http.ResponseWriter, err error) {
	ref, ok := err.(task.Refusal)
	if !ok {
		writeRefusal(w, http.StatusInternalServerError, "dispatch_failed", err.Error())
		return
	}
	status := http.StatusBadRequest
	switch ref.Code {
	case "claims_required":
		// 422, not 400. The body parsed and the fields are the right shape;
		// what is missing is a declaration, and the difference tells a caller
		// whether to fix their serialiser or their intent.
		status = http.StatusUnprocessableEntity
	case "workspace_busy":
		status = http.StatusConflict
	}
	writeRefusal(w, status, ref.Code, ref.Detail)
}
