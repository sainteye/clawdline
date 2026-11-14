package projectassistant

import "testing"

// As in Codync, a Bot works in one of its agent's modes, Auto when unset, and
// the permission setting can change after creation; an older console that
// sends no policy leaves it as it is.
func TestModesAreTheAgentsAndEditableAfterCreation(t *testing.T) {
	if NativeMode(ClaudeCode, "") != "auto" || NativeMode(Codex, AutoMode) != "agent" || NativeMode(ClaudeCode, "plan") != "plan" {
		t.Fatal("native mode ids")
	}
	if ValidateMode(ClaudeCode, "plan") != nil || ValidateMode(Codex, "read-only") != nil || ValidateMode(ClaudeCode, "read-only") == nil || ValidateMode(ClaudeCode, "bypassPermissions") == nil {
		t.Fatal("mode validation")
	}
	bot := Bot{Agent: ClaudeCode, PermissionPolicy: Ask, Name: "Bot", Appearance: Appearance{Shape: Blob, Color: "#e39567"}, Version: 1}
	next, err := ApplyEdit(bot, EditBot{ExpectedVersion: 1, Name: "Bot", Appearance: bot.Appearance, PermissionPolicy: Approve})
	if err != nil || next.PermissionPolicy != Approve || next.Mode != "" {
		t.Fatalf("approve: %+v %v", next, err)
	}
	next, err = ApplyEdit(next, EditBot{ExpectedVersion: 2, Name: "Bot", Appearance: bot.Appearance, PermissionPolicy: Ask, Mode: "plan"})
	if err != nil || next.PermissionPolicy != Ask || next.Mode != "plan" {
		t.Fatalf("plan: %+v %v", next, err)
	}
	kept, err := ApplyEdit(next, EditBot{ExpectedVersion: 3, Name: "Renamed", Appearance: bot.Appearance})
	if err != nil || kept.Mode != "plan" || kept.PermissionPolicy != Ask {
		t.Fatalf("an edit without a policy changed it: %+v %v", kept, err)
	}
	if _, err = ApplyEdit(next, EditBot{ExpectedVersion: 3, Name: "Bot", Appearance: bot.Appearance, PermissionPolicy: Ask, Mode: "read-only"}); err == nil {
		t.Fatal("a mode Claude Code does not offer was accepted")
	}
}
