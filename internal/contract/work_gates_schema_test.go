package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type contractSchemaDocument struct {
	Defs map[string]contractSchemaDefinition `json:"$defs"`
}

type contractSchemaDefinition struct {
	Properties map[string]contractSchemaProperty `json:"properties"`
}

type contractSchemaProperty struct {
	Default    any                               `json:"default"`
	MaxItems   int                               `json:"maxItems"`
	MaxLength  int                               `json:"maxLength"`
	Maximum    int64                             `json:"maximum"`
	Items      *contractSchemaProperty           `json:"items"`
	Properties map[string]contractSchemaProperty `json:"properties"`
}

func TestWorkGateSchemasCarryDefaultsAndWireBounds(t *testing.T) {
	settings := readContractSchema(t, "settings.schema.json")
	for _, definition := range []string{"SettingsSnapshot"} {
		props := settings.Defs[definition].Properties
		if got, ok := props["planning_gate"].Default.(bool); !ok || !got {
			t.Errorf("%s planning_gate default = %#v, want true", definition, props["planning_gate"].Default)
		}
		if got, ok := props["verify_gate"].Default.(bool); !ok || got {
			t.Errorf("%s verify_gate default = %#v, want false", definition, props["verify_gate"].Default)
		}
	}

	gates := readContractSchema(t, "work-gates.schema.json")
	resultClaims := gates.Defs["WorkGateResult"].Properties["claims"]
	if resultClaims.MaxItems != WorkGateClaimsPerRoundLimit {
		t.Errorf("claims maxItems = %d, want %d", resultClaims.MaxItems, WorkGateClaimsPerRoundLimit)
	}
	claimEvidence := gates.Defs["WorkGateClaim"].Properties["evidence"]
	if claimEvidence.MaxItems != WorkGateEvidenceStringsPerClaimLimit || claimEvidence.Items == nil ||
		claimEvidence.Items.MaxLength != WorkGateEvidenceStringBytesLimit {
		t.Errorf("evidence bounds = %#v", claimEvidence)
	}
	if got := gates.Defs["WorkGateDetailRead"].Properties["recent_rounds"].MaxItems; got != WorkGateRecentRoundsPerItemReadLimit {
		t.Errorf("recent_rounds maxItems = %d, want %d", got, WorkGateRecentRoundsPerItemReadLimit)
	}
	if got := gates.Defs["WorkGateEvidenceSubmission"].Properties["byte_count"].Maximum; got != WorkGateEvidenceArtifactBytesLimit {
		t.Errorf("artifact maximum = %d, want %d", got, WorkGateEvidenceArtifactBytesLimit)
	}
}

func readContractSchema(t *testing.T, name string) contractSchemaDocument {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "v1", name))
	if err != nil {
		t.Fatal(err)
	}
	var doc contractSchemaDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
