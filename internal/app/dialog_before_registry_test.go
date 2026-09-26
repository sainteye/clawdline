package app

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// bypassWarning is the first screen of a scheduled child opened with
// --permission-mode bypassPermissions on a machine that had never accepted
// that mode, as captured on 2026-09-26 (the command line above it trimmed).
// Claude Code draws it before it has a registry entry, and its rows carry no
// numbers.
const bypassWarning = "────────────────────────────────────────────────────────────────────────────────\n" +
	"  WARNING: Claude Code running in Bypass Permissions mode\n" +
	"\n" +
	"  In Bypass Permissions mode, Claude Code will not ask for your approval\n" +
	"  before running potentially dangerous commands.\n" +
	"\n" +
	"  This mode should only be used in a sandboxed container/VM that has\n" +
	"  restricted internet access and can easily be restored if damaged.\n" +
	"\n" +
	"  By proceeding, you accept all responsibility for actions taken while running\n" +
	"  in Bypass Permissions mode.\n" +
	"\n" +
	"  https://code.claude.com/docs/en/security\n" +
	"\n" +
	"  ❯ No, exit\n" +
	"    Yes, I accept\n" +
	"\n" +
	"  Enter to confirm · Esc to cancel\n"

// A dialog drawn before the session has any registry entry reads waiting,
// with its unnumbered rows as buttons, so a person can answer it from the
// console: nothing but a dialog has a caret with no composer under it. A
// flush-left caret over a composer, with no registry saying waiting, is still
// not read as a menu.
func TestADialogBeforeTheRegistryReadsWaitingWithButtons(t *testing.T) {
	row := session.Session{ID: "%70", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateUnknown, Evidence: session.EvidenceProcess}
	term := &menuTerminal{s: row, screen: bypassWarning}

	got := Inventory{Screen: term}.readScreen(context.Background(), row)
	if got.State != session.StateWaiting || got.Menu == nil {
		t.Fatalf("bypass warning: %q with menu %v, want waiting with buttons", got.State, got.Menu)
	}
	m := got.Menu
	if m.Numbered || len(m.Options) != 2 || m.Options[0].Label != "No, exit" ||
		m.Options[1].Label != "Yes, I accept" || m.Selected == nil || *m.Selected != 1 {
		t.Fatalf("bypass warning: menu %+v, want No, exit (selected) / Yes, I accept", m)
	}

	term.show(idleAfterShell)
	if got := (Inventory{Screen: term}).readScreen(context.Background(), row); got.Menu != nil {
		t.Fatalf("empty composer: read menu %+v", got.Menu)
	}
}
