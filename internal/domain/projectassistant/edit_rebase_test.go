package projectassistant

import (
	"slices"
	"testing"
)

func rebaseBot() Bot {
	return Bot{Version: 7, Name: "Xavia", Agent: Codex, PermissionPolicy: Ask, Appearance: Appearance{Shape: Blob, Color: EntryColor}, Instructions: InstructionSnapshot{Text: "old", Revision: 1}}
}

func baseOf(b Bot) *EditBase {
	return &EditBase{Name: b.Name, Appearance: b.Appearance, Instructions: b.Instructions.Text, Agent: b.Agent, PermissionPolicy: b.PermissionPolicy, Mode: b.Mode, Notify: b.Notify, BackgroundThreads: b.BackgroundThreads}
}

// save rebases and applies an edit the way Service.Edit does.
func save(current Bot, in EditBot) (Bot, error) {
	in, err := RebaseEdit(current, in)
	if err != nil {
		return current, err
	}
	return ApplyEdit(current, in)
}

func TestSettingsSaveSucceedsAfterUnrelatedBotActivity(t *testing.T) {
	original := rebaseBot()
	// A reply, a memory write or a background task report advances only the version.
	for _, advanced := range []int64{1, 5, 40} {
		current := original
		current.Version += advanced
		edit := EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "new", PermissionPolicy: Ask, Agent: Codex, Base: baseOf(original)}
		got, err := save(current, edit)
		if err != nil {
			t.Fatalf("version +%d: save refused: %v", advanced, err)
		}
		if got.Instructions.Text != "new" || got.Version != current.Version+1 {
			t.Fatalf("version +%d: saved %+v", advanced, got)
		}
	}
}

func TestSettingsSaveKeepsAFieldChangedElsewhere(t *testing.T) {
	original := rebaseBot()
	current := original
	current.Version++
	current.Name = "Renamed elsewhere"
	got, err := save(current, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "mine", Base: baseOf(original)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Renamed elsewhere" || got.Instructions.Text != "mine" {
		t.Fatalf("stale draft overwrote another change: name %q instructions %q", got.Name, got.Instructions.Text)
	}
}

func TestSettingsSaveNamesTheFieldBothChanged(t *testing.T) {
	original := rebaseBot()
	current := original
	current.Version += 2
	current.Instructions.Text = "theirs"
	current.PermissionPolicy = Approve
	edit := EditBot{ExpectedVersion: original.Version, Name: "Mine too", Appearance: original.Appearance, Instructions: "mine", PermissionPolicy: AutoRead, Base: baseOf(original)}
	_, err := save(current, edit)
	if got := SettingsConflicts(err); !slices.Equal(got, []string{ConflictInstructions, ConflictApproval}) || Code(err) != StaleVersion {
		t.Fatalf("conflict = %v (%v)", got, err)
	}
	// The same value on both sides is not a conflict.
	current.Instructions.Text = "mine"
	current.PermissionPolicy = AutoRead
	if _, err = save(current, edit); err != nil {
		t.Fatalf("agreeing values refused: %v", err)
	}
	current.Instructions.Text = "theirs"
	current.PermissionPolicy = Approve
	edit.OverwriteConflicts = true
	got, err := save(current, edit)
	if err != nil || got.Instructions.Text != "mine" || got.PermissionPolicy != AutoRead || got.Name != "Mine too" {
		t.Fatalf("overwrite = %+v, %v", got, err)
	}
}

func TestSettingsSaveWithoutBaseKeepsTheVersionCheck(t *testing.T) {
	original := rebaseBot()
	current := original
	current.Version++
	_, err := save(current, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "new"})
	if Code(err) != StaleVersion || SettingsConflicts(err) != nil || err.(*Failure).Field != "bot" {
		t.Fatalf("older client: %v", err)
	}
	if _, err = save(original, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "new"}); err != nil {
		t.Fatalf("older client at the current version: %v", err)
	}
}

func TestSettingsSaveOptionalFieldsFollowTheirBase(t *testing.T) {
	original := rebaseBot()
	original.RuntimeSettings = RuntimeSettings{Model: "a"}
	original.FallbackAgents = FallbackConfigOf([]Agent{ClaudeCode})
	base := baseOf(original)
	base.RuntimeSettings = original.RuntimeSettings
	base.FallbackAgents = []Agent{ClaudeCode}
	current := original
	current.Version++
	current.RuntimeSettings = RuntimeSettings{Model: "b"}
	current.Notify = "off"
	unchanged := RuntimeSettings{Model: "a"}
	got, err := save(current, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "x", RuntimeSettings: &unchanged, Notify: "on", Base: base})
	if err != nil || got.RuntimeSettings.Model != "b" || got.Notify != "off" {
		t.Fatalf("unchanged optional fields overwrote newer values: %+v %v", got, err)
	}
	mine := []Agent{Codex}
	current.FallbackAgents = FallbackConfigOf([]Agent{Grok})
	_, err = save(current, EditBot{ExpectedVersion: original.Version, Name: original.Name, Appearance: original.Appearance, Instructions: "x", FallbackAgents: &mine, Base: base})
	if !slices.Equal(SettingsConflicts(err), []string{ConflictFallbacks}) {
		t.Fatalf("fallback conflict = %v", err)
	}
}
