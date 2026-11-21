package projectassistantacp

import (
	"strings"
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

// Claude's ACP adapter names the model its "Default" entry resolves to only in that
// choice's description; settings show it so a person can see which model runs.
func TestConfigurationKeepsChoiceDescriptions(t *testing.T) {
	raw := `{"configOptions":[
		{"id":"model","name":"Model","category":"model","type":"select","currentValue":"default","options":[
			{"value":"default","name":"Default (recommended)","description":"Opus 4.5"},
			{"value":"sonnet","name":"Sonnet"}]},
		{"id":"effort","name":"Effort","category":"thought_level","type":"select","currentValue":"default","options":[
			{"value":"default","name":"Default"},{"value":"high","name":"High","description":"` + strings.Repeat("x", pa.MaxConfigurationValueBytes+1) + `"}]}]}`
	catalog, err := parseConfiguration([]byte(raw), pa.ClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	model := catalog.Options[0]
	if model.Current != "default" || model.Choices[0].Description != "Opus 4.5" || model.Choices[1].Description != "" {
		t.Fatalf("model choices = %+v", model)
	}
	if got := catalog.Options[1].Choices[1]; got.Value != "high" || got.Description != "" {
		t.Fatalf("an oversized note must be dropped, keeping the choice: %+v", got)
	}

	legacy := `{"models":{"currentModelId":"default","availableModels":[{"modelId":"default","name":"Default","description":"Sonnet 4.5"}]}}`
	catalog, err = parseConfiguration([]byte(legacy), pa.ClaudeCode)
	if err != nil {
		t.Fatal(err)
	}
	if got := catalog.Options[0].Choices[0].Description; got != "Sonnet 4.5" {
		t.Fatalf("availableModels description = %q", got)
	}
}
