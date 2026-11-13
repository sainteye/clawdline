package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Explicit owner setup is separate from enabling Bots and from a first send.
// This command only fingerprints already installed files in local state.
func projectAssistantsCommand(ctx context.Context, args []string, state string, stdout, stderr io.Writer) int {
	usage := func() int {
		fmt.Fprint(stderr, cliCopy("misc", "project_assistants.setup_usage", "Usage: clawdline project-assistants setup-claude --node PATH --adapter PATH --cli PATH\n"))
		return 2
	}
	if len(args) < 1 || args[0] != "setup-claude" {
		return usage()
	}
	flags := flag.NewFlagSet("setup-claude", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	node, adapter, cli := flags.String("node", "", ""), flags.String("adapter", "", ""), flags.String("cli", "", "")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *node == "" || *adapter == "" || *cli == "" {
		return usage()
	}
	_, e := projectassistantacp.ConfigureClaude(ctx, state, *node, *adapter, *cli)
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
	if json.NewEncoder(stdout).Encode(map[string]any{"configured": true, "native_tools": "ask_before_acting", "authentication_verified": false, "agent": "claude_code", "adapter_version": projectassistantacp.ClaudeAdapterVersion, "cli_version": projectassistantacp.ClaudeCLIVersion, "adapter_started": false, "model_requests": 0}) != nil {
		returnCode = 1
	}
	return returnCode
}
