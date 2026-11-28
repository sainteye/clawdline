package orchestrator

import (
	"context"

	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
)

func (b *Broker) integrationInstructions(ctx context.Context, root string) (string, error) {
	if b.IntegrationInstructions == nil {
		return "", nil
	}
	return b.IntegrationInstructions(ctx, root)
}

// RecordAudience classifies a broker task record so spawned children and gate
// checkers do not receive interactive Root-only instructions.
func RecordAudience(r Record) projects.LaunchAudience {
	if r.Gate != nil || r.Kind == TaskKindVerificationGate {
		return projects.AudienceGateChecker
	}
	return projects.AudienceChild
}

func (b *Broker) prepareWorkflow(ctx context.Context, root, parent string) (workflowlaunch.Prepared, error) {
	return b.prepareWorkflowForAudience(ctx, root, parent, projects.AudienceRoot)
}

func (b *Broker) prepareWorkflowForAudience(ctx context.Context, root, parent string, audience projects.LaunchAudience) (workflowlaunch.Prepared, error) {
	if b.PrepareWorkflowForAudience != nil {
		return b.PrepareWorkflowForAudience(ctx, root, "", parent, audience)
	}
	if b.PrepareWorkflow == nil {
		return workflowlaunch.Prepared{}, nil
	}
	return b.PrepareWorkflow(ctx, root, "", parent)
}
