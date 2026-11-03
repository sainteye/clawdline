package nextconfig

import (
	"encoding/json"
	"testing"
)

func TestWorkflowSelectionKeepsLegacyConsentAndRejectsInvalidExplicitProfiles(t *testing.T) {
	for _, test := range []struct {
		input, want string
		bad         bool
	}{
		{`{}`, "clawdline", false}, {`{"workflow_skills_enabled":true}`, "superpowers", false},
		{`{"workflow_skills_enabled":false}`, "clawdline", false},
		{`{"workflow_profile":"poteto","workflow_skills_enabled":true}`, "poteto", false},
		{`{"workflow_profile":"clawdline","workflow_skills_enabled":true}`, "clawdline", false},
		{`{"workflow_profile":"alien"}`, "", true}, {`{"workflow_profile":null}`, "", true},
		{`{"workflow_profile":"","workflow_skills_enabled":true}`, "", true},
		{`{"workflow_skills_enabled":"yes"}`, "", true}, {`{"workflow_skills_enabled":null}`, "", true},
	} {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(test.input), &raw); err != nil {
			t.Fatal(err)
		}
		got, err := WorkflowProfile(Values{Raw: raw})
		if got != test.want || (err != nil) != test.bad {
			t.Fatalf("%s => %q %v", test.input, got, err)
		}
	}
}

func TestOldAndNewSettingsWritersStayConsistentAtomically(t *testing.T) {
	f := Open(t.TempDir())
	request := map[string]any{"workflow_profile": "poteto", "graphify_enabled": true}
	if _, err := f.Set(request); err != nil {
		t.Fatal(err)
	}
	if _, mutated := request["workflow_skills_enabled"]; mutated {
		t.Fatal("mutated caller request")
	}
	values, err := f.Set(map[string]any{"workflow_skills_enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	got, err := WorkflowProfile(values)
	graph, _ := values.Bool("graphify_enabled")
	if err != nil || got != "clawdline" || !graph {
		t.Fatalf("legacy writer did not switch only its own setting: %s %v", got, err)
	}
	values, err = f.Set(map[string]any{"workflow_profile": "superpowers"})
	if err != nil {
		t.Fatal(err)
	}
	legacy, _ := values.Bool("workflow_skills_enabled")
	if !legacy {
		t.Fatal("old reader cannot see selected Superpowers")
	}
}
