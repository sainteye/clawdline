package projectassistant

// MaxNetworkReceipts counts every retained effect, including terminal and
// unknown records. Reaching it refuses new work instead of erasing replay facts.
const MaxNetworkReceipts = 64
const MaxNetworkGrants = 64

// Cloud transport evidence is retained independently of native effect receipts.
const MaxNetworkCloudReceipts = 64
const MaxNetworkCloudPending = 8
const MaxNetworkCloudFrames = 16
const MaxNetworkCloudEndpoints = 64
const MaxNetworkCloudLeaseSeconds = 60
const MaxNetworkCloudRefreshSeconds = 20

// Dedicated assistant authority and receipts remain retained after revocation
// or unknown effects, independently of the machine-to-machine Bot namespace.
const MaxNetworkAssistantGrants = 64
const MaxNetworkAssistantReceipts = 64

const NetworkSettingsOperation Operation = "network-settings"

// Network settings are source-owned admission preferences. They confer no
// account, pair or assistant authority and are separate from archive bytes.
type NetworkScope struct {
	AccountID string       `json:"account_id"`
	Key       BotKey       `json:"key"`
	Project   ProjectScope `json:"project"`
}

type NetworkSettings struct {
	Scope                 NetworkScope `json:"scope"`
	Revision              int64        `json:"revision"`
	OfferedToNetwork      bool         `json:"offered_to_network"`
	ReachableByAssistants bool         `json:"reachable_by_assistants"`
}

// Pointer fields preserve settings omitted by an older management client.
type NetworkSettingsPatch struct {
	OfferedToNetwork      *bool `json:"offered_to_network,omitempty"`
	ReachableByAssistants *bool `json:"reachable_by_assistants,omitempty"`
}

type NetworkCapabilityState string

const (
	NetworkReady         NetworkCapabilityState = "ready"
	NetworkSetupRequired NetworkCapabilityState = "setup_required"
	NetworkUnavailable   NetworkCapabilityState = "unavailable"
)

// Capability disclosure comes from current authenticated source composition;
// a saved Bot preference cannot create it. Reason is a stable diagnostic code.
type NetworkCapability struct {
	State  NetworkCapabilityState `json:"state"`
	Reason string                 `json:"reason,omitempty"`
}

type NetworkCapabilities struct {
	Peer       NetworkCapability `json:"peer"`
	Assistants NetworkCapability `json:"assistants"`
}

type NetworkSettingsView struct {
	Fence        Fence               `json:"fence"`
	BotVersion   int64               `json:"bot_version"`
	Settings     NetworkSettings     `json:"settings"`
	Capabilities NetworkCapabilities `json:"capabilities"`
}

type NetworkSettingsWrite struct {
	Scope              NetworkScope         `json:"scope"`
	Fence              Fence                `json:"fence"`
	ExpectedBotVersion int64                `json:"expected_bot_version"` // accepted from old callers; not an edit guard
	ExpectedRevision   int64                `json:"expected_revision"`
	Patch              NetworkSettingsPatch `json:"patch"`
}

func ValidateNetworkScope(scope NetworkScope) error {
	if scope.AccountID == "" || scope.Key.BotID == "" || scope.Key.MachineID == "" {
		return fail(Invalid, "network_scope")
	}
	if err := ValidateScope(scope.Project); err != nil {
		return err
	}
	if scope.Key.MachineID != scope.Project.MachineID {
		return fail(ScopeDenied, "network_machine")
	}
	return nil
}

// ApplyNetworkSettings checks only preference transitions. Its caller must
// independently authenticate the human manager and current source authority.
func ApplyNetworkSettings(current NetworkSettings, patch NetworkSettingsPatch, capabilities NetworkCapabilities) (NetworkSettings, error) {
	if err := ValidateNetworkScope(current.Scope); err != nil {
		return current, err
	}
	if patch.OfferedToNetwork != nil {
		if *patch.OfferedToNetwork && capabilities.Peer.State != NetworkReady {
			return current, fail(Unsupported, "network_peer_setup")
		}
		current.OfferedToNetwork = *patch.OfferedToNetwork
	}
	if patch.ReachableByAssistants != nil {
		if *patch.ReachableByAssistants && capabilities.Assistants.State != NetworkReady {
			return current, fail(Unsupported, "network_assistant_setup")
		}
		current.ReachableByAssistants = *patch.ReachableByAssistants
	}
	if patch.OfferedToNetwork != nil || patch.ReachableByAssistants != nil {
		current.Revision++
	}
	return current, nil
}
