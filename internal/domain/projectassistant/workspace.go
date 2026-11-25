package projectassistant

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const workspacePrefix = "bot-workspace-"

func IsPersonalWorkspaceID(id string) bool {
	if !strings.HasPrefix(id, workspacePrefix) || len(id) != len(workspacePrefix)+sha256.Size*2 {
		return false
	}
	digest := strings.TrimPrefix(id, workspacePrefix)
	_, err := hex.DecodeString(digest)
	return err == nil && digest == strings.ToLower(digest)
}

// WorkspaceForCreate is stable for a retry. A new create operation gets its
// own directory even if a client reuses a previously deleted Bot ID.
func WorkspaceForCreate(c Command) ProjectSelection {
	digest := sha256.Sum256([]byte(c.Key.MachineID + "\x00" + c.Key.BotID + "\x00" + c.IdempotencyKey + "\x00" + c.ContentHash))
	return ProjectSelection{MachineID: c.Key.MachineID, ProjectID: workspacePrefix + hex.EncodeToString(digest[:])}
}

func PersonalScope(target ProjectSelection) ProjectScope {
	return ProjectScope{MachineID: target.MachineID, ProjectID: target.ProjectID, CanonicalID: target.ProjectID, Kind: PersonalWorkspace}
}

// PersonalGroupScope identifies rooms on one source without assigning them a
// member's workspace or creating a directory for the room itself.
func PersonalGroupScope(machine string) ProjectScope {
	digest := sha256.Sum256([]byte("personal-group\x00" + machine))
	return PersonalScope(ProjectSelection{MachineID: machine, ProjectID: workspacePrefix + hex.EncodeToString(digest[:])})
}

// ModelCatalogBotID names the machine's own model discovery, which is not a
// Bot: no Bot record, conversation or memory has this identity.
const ModelCatalogBotID = "model-catalog"

// ModelCatalogBot is the identity the daemon's model catalog asks an agent
// for its models with (internal/app/modelcatalog). It runs in its own empty
// personal workspace, so no Project's settings shape the answer and no Bot
// needs to be chosen, and it uses the person's own login, as an unconfigured
// Bot would.
func ModelCatalogBot(machine string, agent Agent) Bot {
	digest := sha256.Sum256([]byte("model-catalog\x00" + machine))
	scope := PersonalScope(ProjectSelection{MachineID: machine, ProjectID: workspacePrefix + hex.EncodeToString(digest[:])})
	return Bot{Key: BotKey{MachineID: machine, BotID: ModelCatalogBotID}, Project: scope, ConversationID: ModelCatalogBotID, Agent: agent}
}
