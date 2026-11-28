package projectassistantacp

import (
	"encoding/json"
	"testing"
	"unicode"

	app "github.com/sainteye/clawdline/internal/app/projectassistant"
	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func containsHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// Model-only Bot frames and MCP tool schemas are English-only so every turn
// does not pay twice in tokens for duplicated translations.
func TestBotSystemPromptAndMCPDescriptionsAvoidBilingualDuplication(t *testing.T) {
	regular := systemPrompt(app.OpenRequest{
		Bot:       pa.Bot{Name: "Synthetic", Agent: pa.ClaudeCode},
		Directory: "/workspace/demo",
	})
	if containsHan(regular) {
		t.Fatalf("systemPrompt contains Han characters (bilingual duplication): %s", regular)
	}
	if len(regular) > 7600 {
		t.Fatalf("regular systemPrompt grew past budget: %d chars", len(regular))
	}

	lead := systemPrompt(app.OpenRequest{
		Bot:       pa.Bot{Name: "Lead", Agent: pa.ClaudeCode, Lead: true},
		Directory: "/workspace/demo",
	})
	if containsHan(lead) {
		t.Fatalf("lead systemPrompt contains Han characters (bilingual duplication): %s", lead)
	}
	if len(lead) > 9500 {
		t.Fatalf("lead systemPrompt grew past budget: %d chars", len(lead))
	}

	baseRaw, err := json.Marshal(mcpTools())
	if err != nil {
		t.Fatal(err)
	}
	if len(baseRaw) > 29000 {
		t.Fatalf("base mcpTools() schemas grew past budget: %d bytes", len(baseRaw))
	}

	var allTools []any
	allTools = append(allTools, mcpTools()...)
	allTools = append(allTools, mcpDevServerTools()...)
	allTools = append(allTools, mcpBackgroundTool())
	allTools = append(allTools, mcpTeamTools()...)
	allTools = append(allTools, mcpLeadBotTool())
	allTools = append(allTools, mcpCharterTool(), mcpCharterWorkflowTool(), mcpCharterConflictTool())
	allTools = append(allTools, mcpPlaybookTools()...)
	allTools = append(allTools, mcpBotRequestTools()...)

	raw, err := json.Marshal(allTools)
	if err != nil {
		t.Fatal(err)
	}
	if containsHan(string(raw)) {
		t.Fatalf("MCP tool definitions contain Han characters (bilingual duplication)")
	}
	if len(raw) > 46000 {
		t.Fatalf("all MCP tool schemas grew past budget: %d bytes", len(raw))
	}
}
