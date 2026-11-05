package squad

import (
	"embed"
	"encoding/json"
)

// Keep the exact released definitions when their role bodies change. Skill-only
// predecessors are reconstructed from those old bodies, not the current text.
//
//go:embed builtin_role_history/*.json
var builtinRoleHistory embed.FS

// HistoricalBuiltinRoleDefinition resolves a retained role body by its immutable
// version, including the skill references supported before that body's update.
func HistoricalBuiltinRoleDefinition(current Definition, version string) (Definition, bool) {
	if !current.Builtin || version == "" {
		return Definition{}, false
	}
	raw, err := builtinRoleHistory.ReadFile("builtin_role_history/" + current.ShortID + ".json")
	if err != nil {
		return Definition{}, false
	}
	var old Definition
	if json.Unmarshal(raw, &old) != nil || old.DefinitionID != current.DefinitionID ||
		!old.Builtin || old.Digest != builtinDefinitionDigest(old) || old.Version != "sha256:"+old.Digest {
		return Definition{}, false
	}
	if old.Version == version {
		return old, true
	}
	if previous := PreSkillBuiltinVersion(old); previous.Version == version {
		return previous, true
	}
	if previous, ok := HistoricalBuiltinDefinition(old); ok && previous.Version == version {
		return previous, true
	}
	return Definition{}, false
}
