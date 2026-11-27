package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
	cloudtransport "github.com/sainteye/clawdline/internal/transport/cloud"
)

// A charter workflow is the team's state machine of how one piece of work
// flows (docs/bots.md, "Workflow"). The console shows it and never edits it:
// the lead Bot proposes a change and the person applies it, or a terminal
// Session writes it with PUT …/workflow on the person's words, under this
// machine's authority. Every change is a new charter version.

func wireWorkflow(w *pa.CharterWorkflow) *contract.ProjectAssistantCharterWorkflow {
	if w == nil {
		return nil
	}
	out := &contract.ProjectAssistantCharterWorkflow{States: make([]contract.ProjectAssistantCharterWorkflowState, 0, len(w.States)), Transitions: make([]contract.ProjectAssistantCharterWorkflowTransition, 0, len(w.Transitions))}
	for _, s := range w.States {
		out.States = append(out.States, contract.ProjectAssistantCharterWorkflowState{ID: s.ID, Title: s.Title, BotID: s.BotID, Kind: string(s.Kind), Note: s.Note})
	}
	for _, t := range w.Transitions {
		out.Transitions = append(out.Transitions, contract.ProjectAssistantCharterWorkflowTransition{From: t.From, To: t.To, Label: t.Label, Kind: string(t.Kind)})
	}
	return out
}

func domainWorkflow(w contract.ProjectAssistantCharterWorkflow) pa.CharterWorkflow {
	out := pa.CharterWorkflow{States: make([]pa.WorkflowState, 0, len(w.States)), Transitions: make([]pa.WorkflowTransition, 0, len(w.Transitions))}
	for _, s := range w.States {
		out.States = append(out.States, pa.WorkflowState{ID: s.ID, Title: s.Title, BotID: s.BotID, Kind: pa.WorkflowStateKind(s.Kind), Note: s.Note})
	}
	for _, t := range w.Transitions {
		out.Transitions = append(out.Transitions, pa.WorkflowTransition{From: t.From, To: t.To, Label: t.Label, Kind: pa.WorkflowTransitionKind(t.Kind)})
	}
	return out
}

// sessionAuthed is a request made with this machine's own token on this
// machine, never one a Cloud viewer's request carries in process.
func sessionAuthed(r *http.Request) bool {
	return machineAuthed(r) && !cloudtransport.ViaCloud(r.Context())
}

// sessionCharterMachine is the machine a terminal Session acts for with this
// machine's own token: this daemon's Bots identity, with the Bots host
// running. An empty machine_id names it; another one is refused. The
// Session acts on the person's words in its own conversation, so its write
// is the person's; only a request made on this machine carries the token.
func (s *Server) sessionCharterMachine(machine string) (string, error) {
	i, err := s.projectAssistantIdentity()
	if err != nil {
		return "", err
	}
	if machine == "" {
		machine = i.MachineID
	}
	if machine == "" || machine != i.MachineID {
		return "", paHTTPFailure(pa.ScopeDenied, "machine_id")
	}
	s.projectAssistants.mu.RLock()
	host := s.projectAssistants.host
	s.projectAssistants.mu.RUnlock()
	if host == nil {
		return "", paHTTPFailure(pa.Disabled, "source_feature")
	}
	return machine, nil
}

// charterWorkflowRead answers the charter, its workflow included, for a
// Session's `clawdline charter workflow show`; a person's device reads it as
// it reads GET …/charter.
func (s *Server) charterWorkflowRead(r *http.Request) (contract.ProjectAssistantCharterResponse, error) {
	if !sessionAuthed(r) {
		machine, project, after, err := s.charterReadScope(r)
		if err != nil {
			return contract.ProjectAssistantCharterResponse{}, err
		}
		out, err := s.charterAnswer(r.Context(), machine, project, 0)
		if err != nil {
			return out, err
		}
		return out, after()
	}
	// A Session may leave machine_id out; this machine is the only one.
	fields := []string{"project_id"}
	if r.URL.Query().Has("machine_id") {
		fields = append(fields, "machine_id")
	}
	query, err := paHTTPQuery(r, fields...)
	if err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	if err = pa.ValidateCharterProject(query.Get("project_id")); err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	machine, err := s.sessionCharterMachine(query.Get("machine_id"))
	if err != nil {
		return contract.ProjectAssistantCharterResponse{}, err
	}
	return s.charterAnswer(r.Context(), machine, query.Get("project_id"), 0)
}

func (s *Server) charterWorkflowSave(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTPLimit[contract.ProjectAssistantCharterWorkflowSaveRequest](w, r, pa.MaxCharterBodyBytes)
	if err != nil {
		return nil, err
	}
	flow := domainWorkflow(body.Workflow)
	if err = pa.ValidateCharterWorkflow(flow, nil); err != nil {
		return nil, err
	}
	write := func(ctx context.Context, machine string) error {
		_, err := s.projectAssistantFloorsStore().SaveCharterWorkflow(ctx, projectassistantstore.CharterWorkflowWrite{MachineID: machine, ProjectID: body.ProjectID,
			ExpectedVersion: body.ExpectedVersion, Workflow: flow, Reason: body.Reason, Author: pa.CharterByPerson, IdempotencyKey: body.IdempotencyKey, At: time.Now()})
		return err
	}
	if !sessionAuthed(r) {
		return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error { return write(ctx, body.MachineID) },
			func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
	}
	if body.IdempotencyKey == "" || r.Header.Get("Idempotency-Key") != body.IdempotencyKey {
		return nil, paHTTPFailure(pa.Invalid, "idempotency_key")
	}
	if err = pa.ValidateCharterProject(body.ProjectID); err != nil {
		return nil, err
	}
	machine, err := s.sessionCharterMachine(body.MachineID)
	if err != nil {
		return nil, err
	}
	if err = write(r.Context(), machine); err != nil {
		var f *pa.Failure
		if !errors.As(err, &f) {
			err = errors.Join(paHTTPFailure(pa.OutcomeUnknown, "charter"), err)
		}
		return nil, err
	}
	return s.charterAnswer(context.WithoutCancel(r.Context()), machine, body.ProjectID, 0)
}
