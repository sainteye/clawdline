package projects

import (
	"strings"
	"testing"
)

func decodedIntegrationWords(t *testing.T, command string) []string {
	t.Helper()
	words := shellWords(t, command)
	for i, word := range words {
		if strings.HasPrefix(word, "developer_instructions=") {
			decoded, err := tomlBasic(strings.TrimPrefix(word, "developer_instructions="))
			if err != nil {
				t.Fatal(err)
			}
			words[i] = "developer_instructions=" + decoded
		}
	}
	return words
}

func TestMachineIntegrationsShareOneProviderInstructionWithRoleAndMemory(t *testing.T) {
	const integration = "Optional workflow skill: investigate before patching.\nDon't replace existing rules."
	for _, assistant := range []string{AssistantClaude, AssistantCodex} {
		request := LaunchRequest{ProjectRoot: "/project", Assistant: assistant,
			SquadPromptPath: "/state/squad/prompt.md", Memory: inventedMemory, IntegrationInstructions: integration}
		launch, err := Admit(request)
		if err != nil {
			t.Fatal(err)
		}
		words := decodedIntegrationWords(t, launch.ShellCommand())
		joined := strings.Join(words, "\n")
		if !strings.Contains(joined, integration) || !strings.Contains(joined, inventedMemory) || !strings.Contains(joined, request.SquadPromptPath) {
			t.Fatalf("%s lost role, memory or integration: %q", assistant, words)
		}
		flag := "--append-system-prompt"
		if assistant == AssistantCodex {
			flag = "developer_instructions="
		}
		count := 0
		for _, word := range words {
			if word == flag || assistant == AssistantCodex && strings.HasPrefix(word, flag) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%s emitted %d competing inline instructions: %q", assistant, count, words)
		}
		request.Resume = "0f1e2d3c-0000-4000-8000-000000000002"
		resumed, err := Admit(request)
		if err != nil || strings.Contains(strings.Join(decodedIntegrationWords(t, resumed.ShellCommand()), "\n"), integration) {
			t.Fatalf("resume acquired current machine integration: %v %q", err, resumed.Arguments)
		}
	}
}
