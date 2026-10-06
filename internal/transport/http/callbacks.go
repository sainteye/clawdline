package http

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/app/orchestrator"
	"github.com/sainteye/clawdline/internal/contract"
)

// brokerCallback is POST /v1/orchestrator/callbacks: a root hands a command
// to the daemon and ends its turn; the command's exit wakes it with the same
// completion notice a finished child sends (orchestrator/callback.go).
//
// It needs the orchestrator token, as a dispatch does. That is no new
// privilege: the same token already starts children that run any command.
func (s *Server) brokerCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRefusal(w, http.StatusMethodNotAllowed, "method_not_allowed", "A callback is started with POST.")
		return
	}
	if !machineAuthed(r) {
		writeAuthRefusal(w, http.StatusForbidden, "forbidden", "Starting a callback needs the orchestrator token.")
		return
	}
	var body struct {
		TaskID         string            `json:"task_id"`
		Title          string            `json:"title"`
		Argv           []string          `json:"argv"`
		Dir            string            `json:"dir"`
		Env            map[string]string `json:"env"`
		TimeoutMinutes int               `json:"timeout_minutes"`
		WorkID         string            `json:"work_id"`
		Root           struct {
			SessionID string `json:"session_id"`
			Assistant string `json:"assistant"`
			Label     string `json:"label"`
		} `json:"root"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeRefusal(w, http.StatusBadRequest, "bad_request",
			"The body is {task_id, title, argv, dir, env?, timeout_minutes?, work_id?, root: {session_id, assistant}}: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Minute)
	defer cancel()
	out, err := s.broker.StartCallback(ctx, orchestrator.CallbackRequest{
		TaskID: body.TaskID, Title: body.Title, Argv: body.Argv, Dir: body.Dir, Env: body.Env,
		TimeoutMinutes: body.TimeoutMinutes, WorkID: body.WorkID,
		Root: orchestrator.RootRef{SessionID: body.Root.SessionID, Assistant: body.Root.Assistant, Label: body.Root.Label},
	})
	if err != nil {
		writeBrokerError(w, err)
		return
	}
	writeJSON(w, contract.BrokerDispatchResult{
		OK: true, Task: s.brokerTaskRow(ctx, out.Record), Replayed: out.Replayed, Warnings: brokerWarnings(out.Warnings),
	})
}
