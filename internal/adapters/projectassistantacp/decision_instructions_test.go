package projectassistantacp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// What every agent's Bot must be told about asking the person: the rules
// that keep a peer request from ending in "ask the person to paste this".
var decisionRules = []string{
	"ask_person",
	"permission, authorization or a choice between options",
	"irreversible",
	"Ask ordinary small questions in chat",
	"Never reply asking the requesting Bot to relay",
	"never ask the user to copy or paste authorization text",
	"Go to",
	"do not draft the authorization yourself",
	"their own direct message in your chat",
	"within the scope of your question and their reply",
	"需要使用者允許、授權或在選項中做決定",
	"一般的小問題直接在聊天問",
	"不要請使用者複製貼上授權文字",
	"不要自己代擬授權文字",
}

// The instructions each agent actually receives when its session opens.
func openedInstructions(t *testing.T, agent pa.Agent) (string, map[string]any) {
	t.Helper()
	q := app.OpenRequest{Bot: pa.Bot{Name: "Synthetic assistant", Agent: agent}, Directory: t.TempDir(), Binding: pa.ExecutionBinding{Fence: testFence()}}
	owned := OwnedMCP{Fence: testFence(), URL: "http://127.0.0.1:12345/mcp", BearerToken: "owned", Revoke: func(context.Context) error { return nil }}
	params, err := sessionParameters(q, &owned)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Meta struct {
			Rules        string `json:"rules"`
			SystemPrompt struct {
				Append string `json:"append"`
			} `json:"systemPrompt"`
		} `json:"_meta"`
		SystemPrompt struct {
			Content []struct{ Text string } `json:"content"`
		} `json:"systemPrompt"`
	}
	if err = json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	switch agent {
	case pa.ClaudeCode:
		return wire.Meta.SystemPrompt.Append, params
	case pa.Codex:
		if len(wire.SystemPrompt.Content) != 1 {
			t.Fatalf("Codex system prompt: %s", raw)
		}
		return wire.SystemPrompt.Content[0].Text, params
	case pa.Grok:
		return wire.Meta.Rules, params
	}
	// Gemini receives the same frame as its opening prompt text.
	return systemPrompt(q), params
}

func TestEveryAgentIsToldToAskThePersonWithACard(t *testing.T) {
	for _, agent := range []pa.Agent{pa.ClaudeCode, pa.Codex, pa.Grok, pa.Gemini} {
		t.Run(string(agent), func(t *testing.T) {
			text, _ := openedInstructions(t, agent)
			for _, rule := range decisionRules {
				if !strings.Contains(text, rule) {
					t.Errorf("%s instructions do not say %q", agent, rule)
				}
			}
		})
	}
}

// Claude Code prompts for MCP tools it is not told to allow, and a peer
// worker's prompt has nobody watching it: ask_person must never need one.
func TestClaudeBotsCanAskThePersonWithoutANativePrompt(t *testing.T) {
	_, params := openedInstructions(t, pa.ClaudeCode)
	options := params["_meta"].(map[string]any)["claudeCode"].(map[string]any)["options"].(map[string]any)
	allowed, _ := options["allowedTools"].([]string)
	if !slices.Contains(allowed, "mcp__clawdline__ask_person") {
		t.Fatalf("ask_person is not an allowed tool: %v", allowed)
	}
}

func TestMessageBotToolTellsTheRequesterWhereThePersonAnswers(t *testing.T) {
	raw, err := json.Marshal(mcpTools())
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct{ Name, Description string }
	if err = json.Unmarshal(raw, &tools); err != nil {
		t.Fatal(err)
	}
	for _, tool := range tools {
		if tool.Name != "message_bot" {
			continue
		}
		for _, rule := range []string{"waiting for the user's decision", "Go to", "do not draft authorization text", "不要自己代擬授權文字"} {
			if !strings.Contains(tool.Description, rule) {
				t.Errorf("message_bot does not say %q", rule)
			}
		}
		return
	}
	t.Fatal("message_bot is missing")
}
