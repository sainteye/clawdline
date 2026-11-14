package projectassistantacp

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"strings"

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
		// As Codync's bot launch: the adapter's own setting sources stay, so
		// the Project's CLAUDE.md, settings, hooks and MCP servers load as in
		// the person's own Claude Code; only native memory and dreaming are
		// turned off and the Host's tools allowed. The user source is the
		// Bot's own CLAUDE_CONFIG_DIR, not the person's ~/.claude.
		options := map[string]any{
			"allowDangerouslySkipPermissions": false,
			"persistSession":                  q.PersistNativeSession, "promptSuggestions": false, "agentProgressSummaries": false,
			"settings": map[string]any{"autoMemoryEnabled": false, "autoDreamEnabled": false},
		}
		// Native tools stay available under Claude Code's default mode, which
		// decides what to ask about. The three Host-owned tools of this
		// execution's own MCP endpoint never prompt; each still checks its
		// fence and source policy in the Host.
		if owned != nil {
			options["allowedTools"] = []string{"mcp__clawdline__send_message", "mcp__clawdline__dispatch_work", "mcp__clawdline__work_status"}
		}
		params["_meta"] = map[string]any{
			"systemPrompt": map[string]any{"append": systemPrompt(q), "excludeDynamicSections": true},
			"claudeCode":   map[string]any{"options": options},
		}
	case pa.Codex:
		params["systemPrompt"] = map[string]any{
			"mode": "append", "content": []any{map[string]string{"type": "text", "text": systemPrompt(q)}},
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

// systemPrompt is the fixed frame Codync gives every Bot (host/src/chat/
// context.rs render): who it is, where the person talks to it, how replies
// and work reach them, and that its Project is not a sandbox or a grant;
// then the person's standing instructions. The tools' own descriptions
// explain each tool, so they are not repeated here.
func systemPrompt(q app.OpenRequest) string {
	name := strings.TrimSpace(q.Bot.Name)
	engine := "Claude Code"
	if q.Bot.Agent == pa.Codex {
		engine = "Codex"
	}
	lines := []string{"Agent profile:"}
	if name != "" {
		lines = append(lines, "Title: "+name, `Your agent name is "`+name+`". If the user asks for your name, answer with "`+name+`".`)
	}
	lines = append(lines, "",
		"You are a persistent agent the user delegates work to through Clawdline, from their phone or computer.",
		"Environment: you run inside Clawdline. The user talks to you through Clawdline's console, on this computer or through Clawdline Cloud on a phone, not in "+engine+"'s own CLI, IDE or app. Where your base instructions describe "+engine+"'s CLI, terminal, slash commands or keyboard shortcuts, they describe the engine, not where the user is, so don't point the user to them. A chat in Clawdline is one conversation that goes on, with no new session or clear for the user to start, so don't suggest starting over.",
		"",
		"Messages: your final reply is delivered to the user in this chat. Use send_message for something they should read before you finish, such as a result worth seeing now or a question only they can answer. Keep messages short and plain, in the user's language; lead with the result, then what needs the user. Don't send just to acknowledge, to say you started or are still working, or to repeat yourself.",
		"",
		"Work: small edits in the Project you may make with your own tools; an action that needs the user's approval waits on their permission card. Larger changes, or anything to be committed, go through dispatch_work, which runs an isolated worker in its own worktree and reports back in this chat; use work_status to read its result.",
		"",
		"Workspace: your working directory is "+q.Directory+", the Project the user bound you to. For another project, use explicit paths or change directory for that command. This directory is not a sandbox or a grant of access: follow the user's instructions and your permissions, and don't assume other bots' files are yours.",
	)
	if text := strings.TrimSpace(q.Bot.Instructions.Text); text != "" {
		lines = append(lines, "", "Standing instructions from the user:", text)
	}
	return strings.Join(lines, "\n")
}
