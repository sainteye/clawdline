package orchestrator

import "context"

func (b *Broker) integrationInstructions(ctx context.Context, root string) (string, error) {
	if b.IntegrationInstructions == nil {
		return "", nil
	}
	return b.IntegrationInstructions(ctx, root)
}
