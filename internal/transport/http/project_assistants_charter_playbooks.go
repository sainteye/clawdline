package http

import (
	"context"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantstore"
	"github.com/sainteye/clawdline/internal/contract"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Charter playbooks are named Markdown documents of a Project's team charter,
// each assigned to some of the Project's Bots (docs/bots.md, "Playbooks").
// Every change is a new charter version; the answer is the charter after it.

func wirePlaybooks(playbooks []pa.CharterPlaybook) []contract.ProjectAssistantCharterPlaybook {
	out := make([]contract.ProjectAssistantCharterPlaybook, 0, len(playbooks))
	for _, p := range playbooks {
		out = append(out, contract.ProjectAssistantCharterPlaybook{ID: p.ID, Title: p.Title, Text: p.Text, BotIds: append([]string{}, p.BotIDs...)})
	}
	return out
}

// projectBots refuses any Bot id that is not a Bot of the Project on this
// machine: an unknown id and another Project's Bot alike.
func (s *Server) projectBots(ctx context.Context, machine, project string, ids []string) error {
	bots, err := s.projectAssistantFloorsStore().CharterBots(ctx, machine)
	if err != nil {
		return err
	}
	ours := map[string]bool{}
	for _, b := range bots {
		if b.ProjectID == project {
			ours[b.BotID] = true
		}
	}
	for _, id := range ids {
		if !ours[id] {
			return paHTTPFailure(pa.Invalid, "bot_ids")
		}
	}
	return nil
}

func (s *Server) charterPlaybookSave(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTPLimit[contract.ProjectAssistantCharterPlaybookSaveRequest](w, r, pa.MaxCharterBodyBytes)
	if err != nil {
		return nil, err
	}
	playbook := pa.CharterPlaybook{ID: body.Playbook.ID, Title: body.Playbook.Title, Text: body.Playbook.Text, BotIDs: body.Playbook.BotIds}
	if err = pa.ValidateCharterPlaybook(playbook); err != nil {
		return nil, err
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		if err := s.projectBots(ctx, body.MachineID, body.ProjectID, playbook.BotIDs); err != nil {
			return err
		}
		_, err := s.projectAssistantFloorsStore().SaveCharterPlaybook(ctx, projectassistantstore.CharterPlaybookWrite{MachineID: body.MachineID, ProjectID: body.ProjectID,
			ExpectedVersion: body.ExpectedVersion, Playbook: playbook, Reason: body.Reason, Author: pa.CharterByPerson, IdempotencyKey: body.IdempotencyKey, At: time.Now()})
		return err
	}, func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
}

func (s *Server) charterPlaybookRemove(w http.ResponseWriter, r *http.Request) (any, error) {
	if _, err := paHTTPQuery(r); err != nil {
		return nil, err
	}
	body, err := paDecodeHTTP[contract.ProjectAssistantCharterPlaybookRemoveRequest](w, r)
	if err != nil {
		return nil, err
	}
	return s.charterWrite(r, body.MachineID, body.ProjectID, body.IdempotencyKey, func(ctx context.Context) error {
		_, err := s.projectAssistantFloorsStore().RemoveCharterPlaybook(ctx, body.MachineID, body.ProjectID, body.ExpectedVersion, body.ID, body.IdempotencyKey, time.Now())
		return err
	}, func(ctx context.Context) (any, error) { return s.charterAnswer(ctx, body.MachineID, body.ProjectID, 0) })
}
