package orchestrator

import (
	"context"
	"github.com/sainteye/clawdline/internal/adapters/workflowlaunch"
)

func (b *Broker) integrationInstructions(ctx context.Context, root string) (string, error) {
	if b.IntegrationInstructions == nil {
		return "", nil
	}
	return b.IntegrationInstructions(ctx, root)
}

func (b *Broker) prepareWorkflow(ctx context.Context, root, parent string) (workflowlaunch.Prepared, error) {
	if b.PrepareWorkflow == nil {
		return workflowlaunch.Prepared{}, nil
	}
	return b.PrepareWorkflow(ctx, root, "", parent)
}
