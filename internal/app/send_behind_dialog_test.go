package app

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// idleUnderDialog is the bottom of an 80×24 Claude Code pane as it was
// captured on 2026-09-26, the transcript above the dialog replaced: Claude
// Code's own "Teach auto mode" dialog drawn over the composer while its
// registry still said idle. Every line sent to it that morning was pasted into
// the dialog and came back send_unsubmitted.
const idleUnderDialog = "  The build is ready to deploy.\n" +
	"\n" +
	"────────────────────────────────────────────────────────────────────────────────\n" +
	"  Teach auto mode about your environment?\n" +
	"\n" +
	"  Auto mode works better when it knows your environment. Takes about a minute.\n" +
	"\n" +
	"  ❯ 1. Yes\n" +
	"    2. Not now\n" +
	"    3. Don't show again\n" +
	"\n" +
	"  Enter to confirm · Esc to cancel\n" +
	"────────────────────────────────────────────────────────────────────────────────\n" +
	"❯ \n" +
	"────────────────────────────────────────────────────────────────────────────────\n" +
	"  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\n"

// A registry that says idle is not the last word on a screen with a dialog on
// it: the row reads waiting, with the dialog's rows as buttons, so the phone
// can answer it. The same registry idle over an empty composer stays idle.
func TestAnIdleRegistryUnderAClaudeDialogReadsWaiting(t *testing.T) {
	row := session.Session{ID: "%10", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle, Evidence: session.EvidenceRegistry}
	term := &menuTerminal{s: row, screen: idleUnderDialog}

	got := Inventory{Screen: term}.readScreen(context.Background(), row)
	if got.State != session.StateWaiting || got.Evidence != session.EvidenceScreen {
		t.Fatalf("dialog up: %q by %q, want waiting by screen", got.State, got.Evidence)
	}
	if got.Menu == nil || len(got.Menu.Options) != 3 || got.Menu.Options[1].Label != "Not now" {
		t.Fatalf("dialog up: menu %+v, want Yes / Not now / Don't show again", got.Menu)
	}

	term.show(idleAfterShell)
	got = Inventory{Screen: term}.readScreen(context.Background(), row)
	if got.State != session.StateIdle || got.Evidence != session.EvidenceRegistry || got.Menu != nil {
		t.Fatalf("empty composer: %q by %q with menu %v, want idle by registry and no menu",
			got.State, got.Evidence, got.Menu)
	}
}

// sendCounter is a menuTerminal that counts the lines Send was asked to type.
type sendCounter struct {
	*menuTerminal
	sent int
}

func (c *sendCounter) Send(ctx context.Context, s session.Session, text string) error {
	c.sent++
	return nil
}

// A line for a session with a question on screen is refused before a byte is
// typed, as Unsent, so a caller may send it again once the question is
// answered — and it then goes.
func TestASendToASessionAskingAQuestionTypesNothing(t *testing.T) {
	row := session.Session{ID: "%10", Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateIdle, Evidence: session.EvidenceRegistry}
	term := &sendCounter{menuTerminal: &menuTerminal{s: row, screen: idleUnderDialog}}
	actions := menuActions(term.menuTerminal)
	actions.Terminals = []ports.TerminalHost{term}
	actions.Inventory.Terminals = []ports.TerminalHost{term}

	_, err := actions.Send(context.Background(), "%10", "1")
	var refusal Refusal
	if !errors.As(err, &refusal) || refusal.Code != "session_asking" {
		t.Fatalf("send under a dialog: %v, want session_asking", err)
	}
	if !errors.As(err, new(terminal.Unsent)) {
		t.Fatalf("send under a dialog: %v is not Unsent", err)
	}
	if term.sent != 0 || len(term.keys()) != 0 {
		t.Fatalf("typed %d lines and %q under a dialog", term.sent, term.keys())
	}
	if _, err := actions.SendIfIdle(context.Background(), "%10", "1"); err == nil || term.sent != 0 {
		t.Fatalf("SendIfIdle under a dialog: %v, typed %d", err, term.sent)
	}

	term.show(idleAfterShell)
	if _, err := actions.Send(context.Background(), "%10", "1"); err != nil || term.sent != 1 {
		t.Fatalf("send once answered: %v, typed %d, want one line", err, term.sent)
	}
}

func TestCodexRateLimitDialogBlocksSendAndTakesTheChosenOption(t *testing.T) {
	screen, err := os.ReadFile("../domain/session/testdata/menu-codex-rate-limit.txt")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	row := session.Session{ID: "%10", Backend: session.BackendTmux, Assistant: session.AssistantCodex,
		State: session.StateIdle, Evidence: session.EvidenceRegistry}
	term := &sendCounter{menuTerminal: &menuTerminal{s: row, screen: string(screen)}}
	actions := menuActions(term.menuTerminal)
	actions.Terminals = []ports.TerminalHost{term}
	actions.Inventory.Terminals = []ports.TerminalHost{term}

	seen := actions.Inventory.readScreen(ctx, row)
	if seen.State != session.StateWaiting || seen.Menu == nil || len(seen.Menu.Options) != 3 {
		t.Fatalf("rate-limit dialog: state %q, menu %+v", seen.State, seen.Menu)
	}
	var refusal Refusal
	if _, err := actions.Send(ctx, row.ID, "test"); !errors.As(err, &refusal) || refusal.Code != "session_asking" {
		t.Fatalf("send: %v, want session_asking", err)
	}
	if _, err := actions.SubmitTyped(ctx, row.ID, "test"); !errors.As(err, &refusal) || refusal.Code != "input_behind_question" {
		t.Fatalf("submit typed: %v, want input_behind_question", err)
	}
	if term.sent != 0 || len(term.keys()) != 0 {
		t.Fatalf("send typed %d lines and %q at the dialog", term.sent, term.keys())
	}

	expect := session.MenuFingerprint(*seen.Menu)
	const idle = "› Ask Codex to do anything\n\n  fixture · /tmp"
	term.onKey = func(t *menuTerminal, b []byte) {
		if string(b) == "2" {
			t.show(idle)
		}
	}
	if _, err := actions.Key(ctx, row.ID, "2", expect); err != nil {
		t.Fatalf("choose Keep current model: %v", err)
	}
	if keys := term.keys(); len(keys) != 1 || keys[0] != "2" {
		t.Fatalf("typed %q, want exactly the chosen digit", keys)
	}
	if _, err := actions.Key(ctx, row.ID, "2", expect); !errors.As(err, &refusal) || refusal.Code != "menu_moved" {
		t.Fatalf("stale answer: %v, want menu_moved", err)
	}
	if len(term.keys()) != 1 {
		t.Fatalf("stale answer typed more keys: %q", term.keys())
	}
}
