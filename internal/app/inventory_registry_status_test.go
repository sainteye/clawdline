package app

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// idleAfterShell is the bottom of a Claude Code pane whose turn has ended with
// a background shell still running, as it was captured on 2026-09-26: the
// turn's footer names the shell, and the composer below it is waiting.
const idleAfterShell = "  Board item 已結案。\n" +
	"\n" +
	"✻ Cogitated for 11m 39s · done 1:32 PM · 1 shell still running\n" +
	"\n" +
	"────────────────────────────────────────\n" +
	"❯ \n" +
	"────────────────────────────────────────\n" +
	"  ⏵⏵ auto mode on · 1 shell · ← 1 agent\n"

// Claude Code's registry says `shell` for exactly that pane. It is an idle
// session with a build going, and the row already says the build is going.
func TestARegistryShellStatusIsAnIdleSession(t *testing.T) {
	if got := session.StateFromAssistantStatus("shell"); got != session.StateIdle {
		t.Fatalf("shell = %q, want idle", got)
	}
}

// A registry word this build has never heard of must not freeze a row as
// unreadable while its screen plainly shows an empty composer; and an
// unknown word with a screen that proves nothing stays unknown.
func TestAnUnknownRegistryStatusLetsTheScreenAnswer(t *testing.T) {
	row := session.Session{ID: "%1", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateFromAssistantStatus("a-word-from-a-later-build"), Evidence: session.EvidenceRegistry}
	if row.State != session.StateUnknown {
		t.Fatalf("fixture: %q", row.State)
	}

	term := &menuTerminal{s: row, screen: idleAfterShell}
	got := Inventory{Screen: term}.readScreen(context.Background(), row)
	if got.State != session.StateIdle || got.Evidence != session.EvidenceScreen {
		t.Fatalf("empty composer: %q by %q, want idle by screen", got.State, got.Evidence)
	}

	term.show("  some output\n  with no composer drawn\n")
	got = Inventory{Screen: term}.readScreen(context.Background(), row)
	if got.State != session.StateUnknown || got.Evidence != session.EvidenceRegistry {
		t.Fatalf("no evidence: %q by %q, want unknown by registry", got.State, got.Evidence)
	}

	// A registry that knows its word is still never second-guessed.
	idle := row
	idle.State = session.StateIdle
	term.show("  some output\n  with no composer drawn\n")
	if got := (Inventory{Screen: term}).readScreen(context.Background(), idle); got.State != session.StateIdle {
		t.Fatalf("registry idle overruled: %q", got.State)
	}
}
