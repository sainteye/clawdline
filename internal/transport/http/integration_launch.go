package http

import (
	"context"
	"strings"

	"github.com/sainteye/clawdline/internal/adapters/graphify"
	"github.com/sainteye/clawdline/skills"
)

// integrationLaunchText is consulted once before a new Claude/Codex execution.
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
	if on, _ := values.Bool("workflow_skills_enabled"); on {
		parts = append(parts, skills.WorkflowInstructions())
	}
	if on, _ := values.Bool("graphify_enabled"); on {
		text, err := graphify.LaunchInstructions(ctx, root)
		if err != nil {
			return "", err
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "\n\n"), nil
}
