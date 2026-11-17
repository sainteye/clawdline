package projectassistant

// The secret value travels only through the dedicated submission port. Neither
// a command nor its canonical digest contains value, length or content hash.
const ConnectorCredentialOperation Operation = "connector-credential"

type ConnectorCredentialAction struct {
	Command         Command      `json:"command"`
	Fence           Fence        `json:"fence"`
	ExpectedVersion int64        `json:"expected_version"`
	Ref             ExtensionRef `json:"ref"`
	Action          string       `json:"action"`
	Target          string       `json:"target"`
	Authority       string       `json:"-"`
}

type ConnectorCredential struct {
	State    string             `json:"state"`
	Revision int64              `json:"revision"`
	Handoff  *LoginSecretTarget `json:"handoff,omitempty"`
}
