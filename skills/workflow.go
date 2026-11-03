package skills

import (
	"embed"
	"strings"
)

//go:embed workflow-prefix.md evidence-debugging/SKILL.md evidence-review/SKILL.md
var workflowFiles embed.FS

// WorkflowInstructions is a bundled, local adaptation, never downloaded at
// launch. Its caller must first verify the machine's explicit opt-in.
func WorkflowInstructions() string {
	var parts []string
	for _, name := range []string{"workflow-prefix.md", "evidence-debugging/SKILL.md", "evidence-review/SKILL.md"} {
		body, err := workflowFiles.ReadFile(name)
		if err != nil {
			panic(err) // Build-time embedded assets cannot disappear at runtime.
		}
		parts = append(parts, strings.TrimSpace(string(body)))
	}
	return strings.Join(parts, "\n\n")
}
