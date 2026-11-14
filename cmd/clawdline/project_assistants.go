package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Explicit setup is separate from enabling Bots and from a first send; a first
// send also sets up on demand. It finds the person's Claude Code and Node and
// installs the adapter Clawdline supplies into local state.
func projectAssistantsCommand(ctx context.Context, args []string, state string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "setup-claude" {
		fmt.Fprint(stderr, cliCopy("misc", "project_assistants.setup_usage", "Usage: clawdline project-assistants setup-claude\n"))
		return 2
	}
	i, e := projectassistantacp.SetupClaude(ctx, state)
	if e == nil {
		e = projectassistantacp.CheckClaudeInstallation(ctx, state)
	}
	if e != nil {
		fmt.Fprintf(stderr, cliCopy("misc", "project_assistants.setup_failed", "Claude setup was refused (%s). No adapter was started.\n"), pa.Code(e))
		return 1
	}
	// Fixed machine-readable receipt; paths, credentials and artifact contents
	// are not echoed. Native authentication remains a later runtime observation.
	returnCode := 0
	if json.NewEncoder(stdout).Encode(map[string]any{"configured": true, "native_tools": "ask_before_acting", "authentication_verified": false, "agent": "claude_code", "adapter_version": projectassistantacp.ClaudeAdapterVersion, "cli_version": i.CLIVersion(), "minimum_cli_version": projectassistantacp.MinimumClaudeCLIVersion, "adapter_started": false, "model_requests": 0}) != nil {
		returnCode = 1
	}
	return returnCode
}
