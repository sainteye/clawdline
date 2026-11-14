package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/projectassistantacp"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Explicit setup is separate from enabling Bots and from a first send; a first
// send also sets up on demand. It finds the person's Claude Code and Node and
// installs the adapter Clawdline supplies into local state.
func projectAssistantsCommand(ctx context.Context, args []string, state string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "enable" || args[0] == "disable" || args[0] == "status") {
		return projectAssistantsSwitch(args[0], stdout, stderr, daemonProjectAssistants)
	}
	if len(args) != 1 || args[0] != "setup-claude" && args[0] != "setup-codex" {
		fmt.Fprint(stderr, cliCopy("misc", "project_assistants.setup_usage", "Usage: clawdline project-assistants enable | disable | status | setup-claude | setup-codex\n"))
		return 2
	}
	codex := args[0] == "setup-codex"
	var i projectassistantacp.ClaudeInstallation
	var e error
	if codex {
		if i, e = projectassistantacp.SetupCodex(ctx, state); e == nil {
			e = projectassistantacp.CheckCodexInstallation(ctx, state)
		}
	} else if i, e = projectassistantacp.SetupClaude(ctx, state); e == nil {
		e = projectassistantacp.CheckClaudeInstallation(ctx, state)
	}
	if e != nil {
		fmt.Fprintf(stderr, cliCopy("misc", "project_assistants.setup_failed", "Claude setup was refused (%s). No adapter was started.\n"), pa.Code(e))
		return 1
	}
	// Fixed machine-readable receipt; paths, credentials and artifact contents
	// are not echoed. Native authentication remains a later runtime observation.
	returnCode := 0
	if json.NewEncoder(stdout).Encode(setupReceipt(codex, i.CLIVersion())) != nil {
		returnCode = 1
	}
	return returnCode
}

// daemonProjectAssistants reaches the running daemon's Bots switch with this
// machine's local token, the only credential that route accepts.
func daemonProjectAssistants(method string, body any) (int, []byte, error) {
	req, err := daemonRequest(method, "/v1/settings/project-assistants", body)
	if err != nil {
		return 0, nil, err
	}
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	return res.StatusCode, data, err
}

// projectAssistantsSwitch turns persistent Bots on or off on this machine.
// The feature stays off by default and has no switch in the console, so this
// command is the way a person opts in before it is announced.
func projectAssistantsSwitch(verb string, stdout, stderr io.Writer, call settingsCall) int {
	method, body := http.MethodGet, any(nil)
	if verb != "status" {
		method, body = http.MethodPost, map[string]bool{"enabled": verb == "enable"}
	}
	status, data, err := call(method, body)
	if err != nil {
		fmt.Fprintf(stderr, cliCopy("misc", "project_assistants.switch_unreachable", "The daemon did not answer: %v\n"), err)
		return 1
	}
	var state struct {
		Enabled *bool `json:"enabled"`
	}
	if status != http.StatusOK || json.Unmarshal(data, &state) != nil || state.Enabled == nil {
		fmt.Fprintf(stderr, cliCopy("misc", "project_assistants.switch_refused", "The Bots switch was not changed (HTTP %d).\n"), status)
		return 1
	}
	if *state.Enabled {
		fmt.Fprint(stdout, cliCopy("misc", "project_assistants.switch_on", "Bots are on. Open the console and choose All Bots in the Session list.\n"))
	} else {
		fmt.Fprint(stdout, cliCopy("misc", "project_assistants.switch_off", "Bots are off. Saved Bots and conversations are kept.\n"))
	}
	return 0
}

// setupReceipt is the fixed machine-readable result; it echoes no paths.
func setupReceipt(codex bool, cli string) map[string]any {
	if codex {
		return map[string]any{"configured": true, "agent": "codex", "authentication_verified": false, "adapter_version": projectassistantacp.CodexAdapterVersion, "cli_version": cli, "minimum_cli_version": projectassistantacp.MinimumCodexCLIVersion, "adapter_started": false, "model_requests": 0}
	}
	return map[string]any{"configured": true, "native_tools": "claude_default_mode", "authentication_verified": false, "agent": "claude_code", "adapter_version": projectassistantacp.ClaudeAdapterVersion, "cli_version": cli, "minimum_cli_version": projectassistantacp.MinimumClaudeCLIVersion, "adapter_started": false, "model_requests": 0}
}
