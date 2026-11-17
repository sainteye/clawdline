package projectassistant

import (
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeSettingsPreserveOmittedFieldsAndRotateAgentConfiguration(t *testing.T) {
	b := Bot{Agent: ClaudeCode, Name: "Synthetic", Appearance: Appearance{Shape: Blob, Color: EntryColor}, PermissionPolicy: Ask, Version: 1, Instructions: InstructionSnapshot{Revision: 1}, Notify: "off", RuntimeSettings: RuntimeSettings{Model: "synthetic", Options: map[string]string{"effort": "high"}}}
	edit := EditBot{ExpectedVersion: 1, Name: b.Name, Appearance: b.Appearance}
	old, err := ApplyEdit(b, edit)
	if err != nil || !SameRuntimeSettings(b.RuntimeSettings, old.RuntimeSettings) || old.Notify != "off" || old.Instructions.Revision != 1 {
		t.Fatalf("old client reset optional settings: %+v %v", old, err)
	}
	edit.Agent = Codex
	next, err := ApplyEdit(b, edit)
	if err != nil || next.Agent != Codex || next.Instructions.Revision != 2 || next.RuntimeSettings.Model != "" || next.Project != b.Project {
		t.Fatalf("Agent transition %+v %v", next, err)
	}
	options := RuntimeSettings{Model: "synthetic", Options: map[string]string{"effort": "low"}}
	edit.Agent = ""
	edit.RuntimeSettings = &options
	next, err = ApplyEdit(b, edit)
	if err != nil || next.Instructions.Revision != 2 || !reflect.DeepEqual(next.RuntimeSettings, options) {
		t.Fatalf("model transition %+v %v", next, err)
	}
	if NotifiableCompletion(b, Turn{State: TurnSucceeded}) {
		t.Fatal("disabled Bot notification remained eligible")
	}
	b.Notify = ""
	if !NotifiableCompletion(b, Turn{State: TurnSucceeded}) {
		t.Fatal("legacy notification default changed")
	}
	for _, bad := range []RuntimeSettings{{Model: strings.Repeat("x", MaxConfigurationValueBytes+1)}, {Options: map[string]string{"": "high"}}} {
		if ValidateRuntimeSettings(bad) == nil {
			t.Fatal("invalid setting admitted")
		}
	}
}
