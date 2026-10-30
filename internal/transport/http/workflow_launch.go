package http

import (
	"context"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/skills"
)

func (s *Server) prepareWorkflow(ctx context.Context, root, resume, parent string) (workflowlaunch.Prepared, error) {
	return (workflowlaunch.Service{Store: s.store, Dir: s.cfg.Dir, Resolve: skills.ResolveWorkflow}).Prepare(ctx, root, resume, parent)
}
func (s *Server) fillWorkflowInfo(ctx context.Context, item session.Session, info *contract.SessionInfo) {
	info.Workflow = &contract.SessionWorkflow{State: "legacy_unsnapshotted"}
	if s.store == nil || item.ConversationID == "" {
		return
	}
	snap, found, err := s.store.WorkflowSnapshotForConversation(ctx, item.ConversationID)
	if err != nil {
		info.Workflow.State = "unavailable"
		return
	}
	if !found {
		return
	}
	value := &contract.SessionWorkflow{State: "recorded", ProfileID: snap.ProfileID, Version: snap.Version, Source: snap.Source, Digest: snap.Digest, GraphifyEnabled: &snap.GraphifyEnabled, Stages: []contract.WorkflowStage{}}
	for _, stage := range snap.Stages {
		value.Stages = append(value.Stages, contract.WorkflowStage{ID: stage.ID, Title: stage.Title, Output: stage.Output, Acceptance: stage.Acceptance})
	}
	info.Workflow = value
}
