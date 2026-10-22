package session

import (
	"strings"
	"testing"
)

// Invented content reproduces the measured native frame/footer shapes only.
func agScreen(text, footer string) string {
	return "Antigravity CLI 1.4.5\n" + strings.Repeat("─", 60) + "\n>" + text + "\n" + strings.Repeat("─", 60) + "\n" + footer + "                  Gemini 3.8 Flash · high\n\n"
}

func agPermission() string {
	return "Antigravity CLI 1.4.5\nCommand\n" + strings.Repeat("─", 60) + `
Requesting permission for:
   printf SAMPLE

Run this command?
> 1. Yes, run command
  2. Yes, and always allow in this conversation for commands that start with 'printf'
  3. Yes, and always allow for commands that start with 'printf' (Persist to settings.json)
  4. No, cancel

  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command
esc to cancel                   Gemini 3.8 Flash · high`
}

func TestAntigravityCurrentSurface(t *testing.T) {
	idle := agScreen("", "? for shortcuts")
	work := strings.Replace(agScreen("", "esc to cancel"), "Antigravity CLI 1.4.5\n", "Antigravity CLI 1.4.5\n⣯  Generating...\n", 1)
	for _, tc := range []struct {
		name, screen string
		kind         AntigravityScreenKind
	}{
		{"idle", idle, AntigravityIdle},
		{"working", work, AntigravityWorking},
		{"draft", agScreen(" preserve this draft", ""), AntigravityDraft},
		{"slash", agScreen(" /config", ""), AntigravityUnknown},
		{"bare", ">", AntigravityUnknown},
		{"active error", "Error: synthetic failure\n" + idle, AntigravityIdle},
		{"adjacent active error", strings.Replace(idle, "Antigravity CLI 1.4.5\n", "Antigravity CLI 1.4.5\nError: synthetic failure\n", 1), AntigravityUnknown},
		{"adjacent exhausted quota", strings.Replace(idle, "Antigravity CLI 1.4.5\n", "Antigravity CLI 1.4.5\nQuota exhausted\n", 1), AntigravityUnknown},
		{"no stop", agScreen("", ""), AntigravityUnknown},
		{"permission", agPermission(), AntigravityMenu},
		{"settings", idle + "Settings\n> Agent Mode default\nKeyboard: ↑/↓ Navigate enter Select esc Exit\nGemini 3.8 Flash · high", AntigravityUnknown},
		{"login", idle + "Sign in\n> Continue\nGemini 3.8 Flash · high", AntigravityUnknown},
		{"trust", idle + "Trust this project?\n> Yes\nNo\nGemini 3.8 Flash · high", AntigravityUnknown},
		{"quota", idle + "Quota exhausted\nGemini 3.8 Flash · high", AntigravityUnknown},
		{"old work", "⣯ Generating...\n" + idle, AntigravityIdle},
		{"old permission", agPermission() + "\n" + idle, AntigravityIdle},
		{"unknown width", strings.ReplaceAll(idle, "Gemini 3.8 Flash · high", "Gemini\n3.8 Flash · high"), AntigravityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ReadAntigravityScreen(tc.screen, false); got.Kind != tc.kind {
				t.Fatalf("got %+v want %s", got, tc.kind)
			}
		})
	}
	if ReadAntigravityScreen(agScreen("", ""), true).Kind != AntigravityIdle {
		t.Fatal("verified Stop with empty composer")
	}
	if AntigravitySendReady(idle, "/usage", false) || AntigravitySendReady(work, "reply", true) || AntigravitySendReady(agScreen(" draft", ""), "reply", true) {
		t.Fatal("unsafe ordinary send")
	}
	if !AntigravitySendReady(idle, "reply", false) {
		t.Fatal("idle ordinary text")
	}
	if _, ok := ReadMenu(agPermission(), AssistantClaude, false); ok {
		t.Fatal("native > leaked into Claude heuristics")
	}
}

func TestAntigravityPermissionRevisionPreservesGrants(t *testing.T) {
	facts := ReadAntigravityScreen(agPermission(), false)
	if facts.Kind != AntigravityMenu || facts.Revision == "" || facts.Menu.Numbered || len(facts.Menu.Options) != 4 {
		t.Fatalf("%+v", facts)
	}
	for i, effect := range []AntigravityPermissionEffect{AntigravityAllowOnce, AntigravityAllowConversationPrefix, AntigravityAllowPersistedPrefix, AntigravityCancel} {
		if got, ok := facts.PermissionEffect(i + 1); !ok || got != effect {
			t.Fatal(got, ok)
		}
	}
	for _, changed := range []string{
		strings.ReplaceAll(agPermission(), "printf SAMPLE", "printf DIFFERENT"),
		strings.ReplaceAll(agPermission(), "'printf'", "'echo'"),
		strings.Replace(agPermission(), "> 1.", "  1.", 1),
		strings.Replace(agPermission(), "↑/↓ Navigate", "Enter to confirm", 1),
		strings.Replace(agPermission(), "Command\n", "Quoted example\n", 1),
	} {
		if ReadAntigravityScreen(changed, false).Revision == facts.Revision {
			t.Fatal("changed permission reused revision")
		}
	}
}
