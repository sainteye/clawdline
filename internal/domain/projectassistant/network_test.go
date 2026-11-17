package projectassistant

import "testing"

func TestNetworkPreferencesNeverEnableMissingInfrastructure(t *testing.T) {
	scope := NetworkScope{AccountID: "fixture-account", Key: BotKey{MachineID: "fixture-machine", BotID: "fixture-bot"}, Project: ProjectScope{MachineID: "fixture-machine", ProjectID: "fixture-project", CanonicalID: "fixture-canonical", Kind: RepositoryProject}}
	on, off := true, false
	current := NetworkSettings{Scope: scope}
	for _, capabilities := range []NetworkCapabilities{{}, {Peer: NetworkCapability{State: NetworkSetupRequired}, Assistants: NetworkCapability{State: NetworkUnavailable}}} {
		if _, err := ApplyNetworkSettings(current, NetworkSettingsPatch{OfferedToNetwork: &on}, capabilities); Code(err) != Unsupported {
			t.Fatalf("peer enabled without infrastructure: %v", err)
		}
		if _, err := ApplyNetworkSettings(current, NetworkSettingsPatch{ReachableByAssistants: &on}, capabilities); Code(err) != Unsupported {
			t.Fatalf("assistant enabled without infrastructure: %v", err)
		}
	}
	ready := NetworkCapabilities{Peer: NetworkCapability{State: NetworkReady}, Assistants: NetworkCapability{State: NetworkReady}}
	current, err := ApplyNetworkSettings(current, NetworkSettingsPatch{OfferedToNetwork: &on, ReachableByAssistants: &on}, ready)
	if err != nil || !current.OfferedToNetwork || !current.ReachableByAssistants || current.Revision != 1 {
		t.Fatalf("configured preference: %+v %v", current, err)
	}
	preserved, err := ApplyNetworkSettings(current, NetworkSettingsPatch{}, NetworkCapabilities{})
	if err != nil || preserved != current {
		t.Fatalf("omitted fields changed preference: %+v %v", preserved, err)
	}
	disabled, err := ApplyNetworkSettings(current, NetworkSettingsPatch{OfferedToNetwork: &off}, NetworkCapabilities{})
	if err != nil || disabled.OfferedToNetwork || !disabled.ReachableByAssistants || disabled.Revision != 2 {
		t.Fatalf("revocation needs no infrastructure: %+v %v", disabled, err)
	}
}
