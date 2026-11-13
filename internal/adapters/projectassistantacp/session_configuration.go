package projectassistantacp

import (
	"context"
	"encoding/json"
	"net"
	"net/url"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// OwnedMCP is a host-issued, revocable capability for exactly one execution.
// It carries no machine credential. Revoke must be idempotent and honor ctx.
// The HTTP owner must revalidate the current actor before every tool effect;
// copying this descriptor does not grant authority to another execution.
type OwnedMCP struct {
	Fence       pa.Fence
	URL         string
	BearerToken string
	Revoke      func(context.Context) error
}

func (m OwnedMCP) validate(fence pa.Fence) error {
	if err := pa.CheckFence(m.Fence, fence); err != nil {
		return err
	}
	u, err := url.Parse(m.URL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() == "" {
		return refusal(pa.ScopeDenied, "owned_mcp_endpoint")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() || m.BearerToken == "" || m.Revoke == nil {
		return refusal(pa.ScopeDenied, "owned_mcp_authority")
	}
	for _, r := range m.BearerToken {
		if r < '!' || r > '~' {
			return refusal(pa.ScopeDenied, "owned_mcp_authority")
		}
	}
	return nil
}

func (m OwnedMCP) wire() map[string]any {
	return map[string]any{
		"type": "http", "name": "clawdline", "url": m.URL,
		"headers": []any{map[string]string{"name": "Authorization", "value": "Bearer " + m.BearerToken}},
	}
}

// These are request settings, never evidence of native enforcement. Provider
// admission still requires independently verified filesystem/memory controls.
// Claude's preset metadata follows pinned Codync's bot launch; Codex's typed
// systemPrompt follows the installed ACP adapter's append-only protocol.
func sessionParameters(q app.OpenRequest, owned *OwnedMCP) (map[string]any, error) {
	servers := []any{}
	if owned != nil {
		if err := owned.validate(q.Binding.Fence); err != nil {
			return nil, err
		}
		servers = append(servers, owned.wire())
	}
	params := map[string]any{"cwd": q.Directory, "mcpServers": servers}
	switch q.Bot.Agent {
	case pa.ClaudeCode:
		params["_meta"] = map[string]any{
			"systemPrompt": map[string]any{"append": q.Bot.Instructions.Text, "excludeDynamicSections": true},
			"claudeCode": map[string]any{"options": map[string]any{
				"tools": []string{}, "settingSources": []string{}, "strictMcpConfig": true,
				"allowDangerouslySkipPermissions": false,
				"settings":                        map[string]any{"autoMemoryEnabled": false, "autoDreamEnabled": false, "disableAllHooks": true},
			}},
		}
	case pa.Codex:
		params["systemPrompt"] = map[string]any{
			"mode": "append", "content": []any{map[string]string{"type": "text", "text": q.Bot.Instructions.Text}},
		}
	case "":
		// Only package-internal transport fixtures omit a Bot. Provider.Open
		// cannot reach this branch because capability admission rejects it.
	default:
		return nil, refusal(pa.Unsupported, "agent")
	}
	return params, nil
}

func adapterPackage(agent pa.Agent) string {
	switch agent {
	case pa.ClaudeCode:
		return "@agentclientprotocol/claude-agent-acp"
	case pa.Codex:
		return "@agentclientprotocol/codex-acp"
	}
	return ""
}

func hasCapability(raw []byte) bool {
	var value map[string]json.RawMessage
	return json.Unmarshal(raw, &value) == nil && value != nil
}
