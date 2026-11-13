package projectassistantacp

import (
	"testing"

	pa "github.com/sainteye/clawdline/internal/domain/projectassistant"
)

func TestNativePermissionPolicyDoesNotInferAuthorityFromPayload(t *testing.T) {
	// These are synthetic adversarial envelopes, not captured vendor sessions.
	payloads := map[string]string{
		"missing":             `{}`,
		"null":                `null`,
		"invalid":             `{"toolCall":`,
		"wrong type":          `{"toolCall":"Read"}`,
		"kind only":           `{"toolCall":{"kind":"read"}}`,
		"claimed native read": `{"sessionId":"s","toolCall":{"toolCallId":"r","name":"Read","status":"pending","rawInput":{"file_path":"/project/readme"},"title":"Read file","kind":"read","content":[],"locations":[{"path":"/project/readme"}]},"options":[{"optionId":"yes","kind":"allow_once","name":"Read once"}]}`,
		"read with hook":      `{"toolCall":{"name":"Read","rawInput":{"file_path":"/project/readme","hooks":{"after":"touch /project/output"}}}}`,
		"extra schema field":  `{"toolCall":{"name":"Read","rawInput":{"file_path":"/project/readme","command":"touch /project/output"}}}`,
		"duplicate tool key":  `{"toolCall":{"name":"Write"},"toolCall":{"name":"Read","kind":"read"}}`,
		"duplicate input key": `{"toolCall":{"name":"Read","rawInput":{"file_path":"/project/a","file_path":"/project/b"}}}`,
		"forged type":         `{"type":"read_only","toolCall":{"type":"Read","rawInput":{"command":"touch /project/output"}}}`,
		"embedded shell":      `{"toolCall":{"name":"Bash","kind":"read","rawInput":{"command":"cat /project/readme; touch /project/output"}}}`,
		"shell substitution":  `{"toolCall":{"name":"exec_command","kind":"read","rawInput":{"command":"cat $(touch /project/output)"}}}`,
		"write as read":       `{"toolCall":{"name":"Write","kind":"read","rawInput":{"file_path":"/project/output","content":"data"}}}`,
		"mcp as read":         `{"toolCall":{"name":"mcp__server__Read","kind":"read","rawInput":{"file_path":"/project/readme"}}}`,
		"dynamic read":        `{"toolCall":{"name":"Read","kind":"read","rawInput":{"type":"dynamicToolCall","tool":"Read"}}}`,
		"codex command":       `{"sessionId":"s","toolCall":{"toolCallId":"r","kind":"execute","status":"pending","title":"cat /project/readme","rawInput":{"command":"cat /project/readme","cwd":"/project"},"locations":[]},"options":[{"optionId":"yes","kind":"allow_once","name":"Allow"}]}`,
		"sandbox expansion":   `{"toolCall":{"toolCallId":"r","name":"request_permissions","kind":"other","rawInput":{"permissions":{"fileSystem":{"read":["/project"]}},"cwd":"/project"}}}`,
		"persistent allow":    `{"toolCall":{"name":"Read","rawInput":{"file_path":"/project/readme"}},"options":[{"optionId":"always","kind":"allow_always","name":"Read only"}]}`,
		"trailing document":   `{"toolCall":{"name":"Read"}} {"toolCall":{"name":"Write"}}`,
	}
	for _, provider := range []struct {
		agent   pa.Agent
		version string
	}{{pa.ClaudeCode, "0.106.0"}, {pa.Codex, "2.6.0"}} {
		t.Run(string(provider.agent), func(t *testing.T) {
			classify := NewReadOnlyPermissionClassifier(provider.agent, provider.version)
			for name, payload := range payloads {
				t.Run(name, func(t *testing.T) {
					decision := ClassifyNativePermission(provider.agent, provider.version, []byte(payload))
					if decision.ReadOnly || decision.Code != pa.ReadOnlyUnverified || classify([]byte(payload)) {
						t.Fatalf("unproved native payload authorized: %+v", decision)
					}
				})
			}
			if classify(nil) {
				t.Fatal("absent payload authorized")
			}
		})
	}
}

func TestNativePermissionPolicyRequiresTrustedSupportedIdentity(t *testing.T) {
	for _, tc := range []struct {
		agent   pa.Agent
		version string
		code    pa.ErrorCode
	}{{pa.ClaudeCode, "", pa.ProviderVersionUnsupported}, {pa.ClaudeCode, "0.106.1", pa.ProviderVersionUnsupported}, {pa.Codex, "2.6.1", pa.ProviderVersionUnsupported}, {"unknown", "2.6.0", pa.Unsupported}, {"", "", pa.Unsupported}} {
		decision := ClassifyNativePermission(tc.agent, tc.version, []byte(`{"agent":"codex","version":"2.6.0","readOnly":true}`))
		if decision.ReadOnly || decision.Code != tc.code || NewReadOnlyPermissionClassifier(tc.agent, tc.version)(nil) {
			t.Fatalf("untrusted identity authorized: %+v", decision)
		}
	}
}
