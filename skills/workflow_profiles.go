package skills

import (
	"embed"
	"fmt"
	"github.com/sainteye/clawdline/internal/domain/workflow"
)

//go:embed poteto-workflow.md
var profileFiles embed.FS

// ResolveWorkflow returns local, versioned content. It never installs or fetches dependencies.
func ResolveWorkflow(id string, graphify bool) (workflow.Snapshot, error) {
	s := workflow.Snapshot{Schema: 1, ProfileID: id, Version: "1", Source: "Clawdline", GraphifyEnabled: graphify, Stages: []workflow.Stage{}}
	switch id {
	case "clawdline":
		// Compatibility mode adds no workflow instruction or compulsory stages.
	case "superpowers":
		s.Source = "Superpowers selected debugging and review skills (Clawdline adaptation)"
		s.Instructions = WorkflowInstructions()
	case "poteto":
		s.Source = "cursor/plugins pstack 0.15.36 @ c742dd61706fbacc1b95f1a20b7b988cf096ce6c (Clawdline adaptation)"
		body, err := profileFiles.ReadFile("poteto-workflow.md")
		if err != nil {
			return workflow.Snapshot{}, err
		}
		s.Instructions = string(body)
	default:
		return workflow.Snapshot{}, fmt.Errorf("invalid_workflow_profile: %s", id)
	}
	if id != "clawdline" {
		s.Stages = []workflow.Stage{
			{ID: "brief", Title: "Task", Output: "Goal, scope and constraints", Acceptance: "Observable acceptance criteria are identified"},
			{ID: "plan", Title: "Plan", Output: "Verifiable work units and dependencies", Acceptance: "Each unit has an output and a check"},
			{ID: "execute", Title: "Execute", Output: "Changes or investigation findings", Acceptance: "Work remains within the authorized scope"},
			{ID: "verify", Title: "Verify", Output: "Evidence mapped to each criterion", Acceptance: "Pass, failure and human judgment are explicit"},
			{ID: "deliver", Title: "Deliver", Output: "Exact revision, results and remaining risks", Acceptance: "Claims match the actual evidence and authorized destinations"},
		}
	}
	return workflow.Seal(s)
}
