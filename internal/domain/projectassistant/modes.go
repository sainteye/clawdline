package projectassistant

import "strings"

// AutoMode is Codync's id for an agent's Auto mode, whatever the agent calls
// it (apps/desktop/src/shared/approvals.ts AUTO_MODE); new Bots start there.
const AutoMode = "auto"

// AgentMode is one mode an agent offers, as its ACP session reports it.
type AgentMode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// AgentModes lists the modes of the supplied adapters, Auto first, as their
// pinned versions report them (claude-agent-acp 0.106.0 session-mode.js,
// codex-acp 2.6.0 AgentMode). Claude's bypassPermissions is not offered by
// its adapter unless the person's settings allow it, so it is not listed.
func AgentModes(agent Agent) []AgentMode {
	switch agent {
	case ClaudeCode:
		return []AgentMode{
			{ID: AutoMode, Name: "Auto", Description: "Claude handles permission decisions"},
			{ID: "default", Name: "Manual", Description: "Always ask before making changes"},
			{ID: "acceptEdits", Name: "Accept edits", Description: "Automatically accept all file edits"},
			{ID: "plan", Name: "Plan", Description: "Create a plan before making changes"},
		}
	case Codex:
		return []AgentMode{
			{ID: AutoMode, Name: "Auto review", Description: "Only ask for actions detected as potentially unsafe"},
			{ID: "read-only", Name: "Read-only", Description: "Requires approval to edit files and access the internet."},
			{ID: "workspace-write", Name: "Workspace access", Description: "Edit workspace files; ask before writing outside the workspace or accessing the network."},
			{ID: "agent-full-access", Name: "Full access", Description: "Unrestricted access to the internet and any file on your computer"},
		}
	}
	return nil
}

// NativeMode is the agent's own id for a Bot's mode.
func NativeMode(agent Agent, mode string) string {
	if mode == "" || mode == AutoMode {
		if agent == Codex {
			return "agent"
		}
		return "auto"
	}
	return mode
}

// ValidateMode accepts a mode the agent offers, or empty for Auto.
func ValidateMode(agent Agent, mode string) error {
	if mode == "" {
		return nil
	}
	for _, m := range AgentModes(agent) {
		if m.ID == mode {
			return nil
		}
	}
	return fail(Invalid, "mode")
}

// PermissionRule is one standing rule an agent keeps after Always allow or
// Never, as the agent itself spells it (Claude Code: "Bash(git status:*)").
type PermissionRule struct {
	Behavior string `json:"behavior"`
	Rule     string `json:"rule"`
}

// ValidateRule accepts an allow or deny rule with non-empty text.
func ValidateRule(r PermissionRule) error {
	if (r.Behavior != "allow" && r.Behavior != "deny") || strings.TrimSpace(r.Rule) == "" || len(r.Rule) > MaxMessageBytes {
		return fail(Invalid, "rule")
	}
	return nil
}
