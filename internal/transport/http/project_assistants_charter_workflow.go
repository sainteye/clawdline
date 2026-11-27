package http

import (
	"context"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
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
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		_, err := s.projectAssistantFloorsStore().SaveCharterWorkflow(ctx, projectassistantstore.CharterWorkflowWrite{MachineID: body.MachineID, ProjectID: body.ProjectID,
			ExpectedVersion: body.ExpectedVersion, Workflow: flow, Reason: body.Reason, Author: pa.CharterByPerson, IdempotencyKey: body.IdempotencyKey, At: time.Now()})
		return err
	}, func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
}
