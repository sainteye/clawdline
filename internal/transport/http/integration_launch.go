package http

import (
	"context"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/codegraph"
	"github.com/sainteye/clawdline/internal/adapters/nextconfig"
	"github.com/sainteye/clawdline/internal/domain/workflow"
	"github.com/sainteye/clawdline/skills"
)

// integrationLaunchText is consulted once before a new assistant execution.
// It never edits global assistant configuration or the selected repository.
func (s *Server) integrationLaunchText(ctx context.Context, root string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	values, err := s.settingsFile().Read()
	if err != nil {
		return "", err
	}
	var parts []string
	id, err := nextconfig.WorkflowProfile(values)
	if err != nil {
		return "", err
	}
	profile, err := skills.ResolveWorkflow(id, false)
	if err != nil {
		return "", err
	}
	if profile.Instructions != "" {
		parts = append(parts, profile.Instructions)
	}
	if on, _ := values.Bool("codegraph_enabled"); on {
		text, err := codegraph.LaunchInstructions(ctx, root)
		if err != nil {
			return "", err
		}
		parts = append(parts, text)
	}
	if on, _ := values.Bool("space_saving_images"); on {
		parts = append(parts, workflow.SpaceSavingImageInstructions())
	}
	if on, _ := values.Bool("browser_enabled"); on {
		parts = append(parts, workflow.BrowserUseInstructions())
	}
	return strings.Join(parts, "\n\n"), nil
}
