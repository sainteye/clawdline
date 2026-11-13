package projectassistantacp

import pa "github.com/sainteye/clawdline/internal/domain/projectassistant"

// NativePermissionDecision describes payload classification, not provider
// admission. No inspected adapter envelope currently proves read-only effects.
type NativePermissionDecision struct {
	ReadOnly bool
	Code     pa.ErrorCode
}

// ClassifyNativePermission fails closed for the inspected adapter versions.
// Neither a tool name nor a command's display classification establishes its
// effects, sandbox or hooks. In particular, parsing an asserted "Read" schema
// would not supply the missing native enforcement evidence. Payload bytes are
// deliberately never interpreted as authorization, including malformed JSON,
// duplicate keys and future fields. See project-assistants-native-permissions.md.
func ClassifyNativePermission(agent pa.Agent, adapterVersion string, _ []byte) NativePermissionDecision {
	switch agent {
	case pa.ClaudeCode:
		if adapterVersion != "0.106.0" {
			return NativePermissionDecision{Code: pa.ProviderVersionUnsupported}
		}
	case pa.Codex:
		if adapterVersion != "2.6.0" {
			return NativePermissionDecision{Code: pa.ProviderVersionUnsupported}
		}
	default:
		return NativePermissionDecision{Code: pa.Unsupported}
	}
	return NativePermissionDecision{Code: pa.ReadOnlyUnverified}
}

// NewReadOnlyPermissionClassifier binds trusted adapter identity to the payload
// callback accepted by ProviderOptions. Identity must come from host evidence,
// never from fields asserted by the permission request. This callback does not
// replace Verify or authorize native startup.
func NewReadOnlyPermissionClassifier(agent pa.Agent, adapterVersion string) func([]byte) bool {
	return func(payload []byte) bool {
		return ClassifyNativePermission(agent, adapterVersion, payload).ReadOnly
	}
}
