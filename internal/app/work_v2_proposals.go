package app

import (
	"context"
	"github.com/sainteye/clawdline/internal/adapters/store"
	"github.com/sainteye/clawdline/internal/domain/work"
	"net/http"
	"strings"
)

func (w *WorkSystemV2) Propose(ctx context.Context, p work.ProposalV2) (work.ProposalV2, error) {
	p.ID, p.Title, p.Description, p.Reason = newWorkID(), strings.TrimSpace(p.Title), strings.TrimSpace(p.Description), strings.TrimSpace(p.Reason)
	p.ProjectID, p.ProjectPath, p.SessionID = strings.TrimSpace(p.ProjectID), strings.TrimSpace(p.ProjectPath), strings.TrimSpace(p.SessionID)
	if !p.Kind.Valid() || p.ProjectID == "" || p.ProjectPath == "" || p.SessionID == "" || p.Title == "" || p.Description == "" || p.Reason == "" || strings.TrimSpace(p.SuggestedAcceptance) == "" {
		return p, workV2Error(http.StatusUnprocessableEntity, "invalid_proposal", "A proposal needs Project, kind, title, description, reason, suggested acceptance, and Session; explain the work for a person in plain language.")
	}
	if err := validateWorkV2Text(p.Title, p.Description); err != nil {
		return p, err
	}
	if len(p.Reason) > directTodoTextLimit {
		return p, workV2Error(http.StatusRequestEntityTooLarge, "proposal_too_large", "Proposal reason is at most 8 KiB.")
	}
	if err := validateWorkV2Acceptance(p.SuggestedAcceptance); err != nil {
		return p, err
	}
	if p.SourceWorkID == "" && p.SourceTodoID == "" {
		return p, workV2Error(http.StatusUnprocessableEntity, "proposal_source_required", "A proposal must name the item or direct to-do that revealed it.")
	}
	p.State, p.CreatedAt, p.Version = "pending", w.now(), 1
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		if p.SourceWorkID != "" {
			item, err := tx.Item(p.SourceWorkID)
			if err != nil || item.OwnerSession != p.SessionID {
				return work.RefuseV2("proposal_source_invalid", "The source item is not owned by this Session.")
			}
		}
		if p.SourceTodoID != "" {
			td, err := tx.DirectTodo(p.SourceTodoID)
			if err != nil || td.SessionID != p.SessionID {
				return work.RefuseV2("proposal_source_invalid", "The source to-do does not belong to this Session.")
			}
		}
		return tx.CreateProposal(p)
	})
	return p, mapWorkV2Error(err)
}

func (w *WorkSystemV2) Proposals(ctx context.Context, state string) ([]work.ProposalV2, bool, error) {
	rows, truncated, err := w.Store.WorkV2Proposals(ctx, state, WorkV2PageSize)
	return rows, truncated, mapWorkV2Error(err)
}

type ProposalDecisionV2 struct {
	Decision            string
	Title               string
	Description         string
	SuggestedAcceptance string
}

func (w *WorkSystemV2) ResolveProposal(ctx context.Context, id, actor string, c ProposalDecisionV2, file WorkV2Filer) (WorkV2View, error) {
	var out WorkV2View
	err := w.Store.WriteWorkV2(ctx, func(tx *store.WorkV2Tx) error {
		p, err := tx.Proposal(id)
		if err != nil {
			return work.RefuseV2("proposal_not_found", "No proposal has that id.")
		}
		if p.State != "pending" {
			return work.RefuseV2("proposal_resolved", "This proposal was already resolved.")
		}
		now := w.now()
		if c.Decision == "reject" {
			if err := tx.ResolveProposal(p, "rejected", "", now); err != nil {
				return err
			}
			return nil
		}
		if c.Decision != "accept" {
			return work.RefuseV2("invalid_decision", "Choose accept or reject.")
		}
		if strings.TrimSpace(c.Title) != "" {
			p.Title = strings.TrimSpace(c.Title)
		}
		if strings.TrimSpace(c.Description) != "" {
			p.Description = strings.TrimSpace(c.Description)
		}
		if c.SuggestedAcceptance != "" {
			p.SuggestedAcceptance = c.SuggestedAcceptance
		}
		if err := validateWorkV2Text(p.Title, p.Description); err != nil {
			return err
		}
		if err := validateWorkV2Acceptance(p.SuggestedAcceptance); err != nil {
			return err
		}
		i := work.ItemV2{ID: newWorkID(), ProjectID: p.ProjectID, ProjectPath: p.ProjectPath, Kind: p.Kind, Title: p.Title,
			Description: p.Description, Phase: work.PhaseCreated, DeploymentPolicy: work.DeployAgentDecides,
			CreatedBy: actor, CreatedAt: now, UpdatedAt: now, Cycle: 1, Version: 1}
		work.SetAcceptance(&i, p.SuggestedAcceptance)
		if err := work.ValidateNewV2(i); err != nil {
			return err
		}
		if err := tx.CreateItem(i, actor, payload(map[string]string{"proposal_id": p.ID})); err != nil {
			return err
		}
		if err := tx.ResolveProposal(p, "accepted", i.ID, now); err != nil {
			return err
		}
		out = WorkV2View{Item: i}
		if file != nil {
			if k, a, ok := file(out); ok {
				return tx.CompleteReceipt(k, a)
			}
		}
		return nil
	})
	return out, mapWorkV2Error(err)
}
