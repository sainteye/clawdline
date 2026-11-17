package projectassistant

import "reflect"

const MaxBotExtensions = 32

type ExtensionRef struct {
	ID          string `json:"id"`
	Fingerprint string `json:"fingerprint"`
}
type ExtensionSelection struct {
	Skills     []ExtensionRef `json:"skills"`
	Connectors []ExtensionRef `json:"connectors"`
}
type ExtensionOffer struct {
	ID                 string `json:"id"`
	Fingerprint        string `json:"fingerprint"`
	Name               string `json:"name"`
	Kind               string `json:"kind"`
	CredentialRequired bool   `json:"credential_required,omitempty"`
}
type ExtensionCatalog struct {
	Items               []ExtensionOffer `json:"items"`
	ConnectorsAvailable bool             `json:"connectors_available"`
}

func SameExtensions(a, b ExtensionSelection) bool {
	return len(a.Skills) == 0 && len(b.Skills) == 0 && len(a.Connectors) == 0 && len(b.Connectors) == 0 || reflect.DeepEqual(a, b)
}
func ValidateExtensions(v ExtensionSelection) error {
	if len(v.Skills)+len(v.Connectors) > MaxBotExtensions {
		return fail(CapacityFull, "bot_extensions")
	}
	seen := map[string]bool{}
	for _, list := range [][]ExtensionRef{v.Skills, v.Connectors} {
		for _, ref := range list {
			if ref.ID == "" || len(ref.ID) > MaxNameRunes || len(ref.Fingerprint) != 64 || seen[ref.ID] {
				return fail(Invalid, "bot_extension")
			}
			seen[ref.ID] = true
		}
	}
	return nil
}
