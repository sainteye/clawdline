package projectassistant

import wire "github.com/sainteye/clawdline/internal/domain/agenthandoff"

const NetworkRequestOrigin TurnOrigin = "network_request"

// NetworkWork is source-captured provenance, never accepted on the human send
// route. It binds the peer request to one native turn across source restarts.
type NetworkWork struct {
	Header    wire.BotHeader `json:"header"`
	Assistant *AssistantWork `json:"assistant,omitempty"`
}

func IsPeerRequest(origin TurnOrigin) bool {
	return origin == BotRequestOrigin || origin == NetworkRequestOrigin
}

type AssistantWork struct {
	Call        wire.AssistantCall `json:"call"`
	Body        string             `json:"body"`
	PrincipalID string             `json:"principal_id"`
}
