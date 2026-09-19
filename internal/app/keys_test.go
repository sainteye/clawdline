package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/sainteye/clawdline-go/internal/app/lane"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The route is a byte channel into a tty, so what it will send is a closed
// list: nine digits, Tab, back-tab and the word `submit`. Anything else —
// text, a control byte, a second escape sequence — is refused before a
// session is looked up.
func TestKeyNameIsAClosedList(t *testing.T) {
	admitted := map[string][]byte{
		"1": {'1'}, "9": {'9'}, "tab": {0x09}, "shift+tab": {0x1b, 0x5b, 0x5a}, "submit": nil,
	}
	for key, want := range admitted {
		got, _, ok := KeyName(key)
		if !ok || !bytes.Equal(got, want) {
			t.Fatalf("%q: %v %v", key, got, ok)
		}
	}
	for _, key := range []string{"", "0", "10", "a", "Tea", "\x03", "\r", "enter", "escape", "\x1b[A", " 1", "１"} {
		if _, _, ok := KeyName(key); ok {
			t.Fatalf("%q was admitted", key)
		}
	}
	// Refused without reading the machine: an Actions with nothing in it
	// would fail differently if the lookup came first.
	_, err := Actions{}.Key(context.Background(), "%1", "Tea", "")
	if ref, ok := err.(Refusal); !ok || ref.Code != "bad_request" {
		t.Fatalf("err %v", err)
	}
}

// permissionPrompt is a Claude Code permission dialog for one command, as a
// pane draws it. Two of them differ only in the command, which is the whole
// point: the rows are the same, and a digit meant for one approves the other.
func permissionPrompt(command string, caret int) string {
	row := func(n int, label string) string {
		mark := "  "
		if n == caret {
			mark = "❯ "
		}
		return fmt.Sprintf("│ %s%d. %-40s│\n", mark, n, label)
	}
	return "✻ Working…\n\n" +
		"╭──────────────────────────────────────────────╮\n" +
		"│ Bash command                                 │\n" +
		"│                                              │\n" +
		"│   " + fmt.Sprintf("%-43s", command) + "│\n" +
		"│                                              │\n" +
		"│ Do you want to proceed?                      │\n" +
		row(1, "Yes") +
		row(2, "Yes, and don't ask again for rm") +
		row(3, "No, and tell Claude what to do (esc)") +
		"╰──────────────────────────────────────────────╯\n"
}

// menuTerminal is one tmux pane with a picker on it: what it shows, what was
// typed at it, and what typing does to what it shows.
type menuTerminal struct {
	mu     sync.Mutex
	s      session.Session
	screen string
	blind  bool
	typed  []string
	onKey  func(t *menuTerminal, b []byte)
}

func (t *menuTerminal) Name() string { return "tmux" }
func (t *menuTerminal) Inventory(ctx context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Sessions: []session.Session{t.s}}, nil
}
func (t *menuTerminal) Open(ctx context.Context, req ports.OpenRequest) (session.Session, error) {
	return session.Session{}, errors.New("no")
}
func (t *menuTerminal) Send(ctx context.Context, s session.Session, text string) error { return nil }
func (t *menuTerminal) Interrupt(ctx context.Context, s session.Session) error         { return nil }
func (t *menuTerminal) Close(ctx context.Context, s session.Session) error             { return nil }
func (t *menuTerminal) Type(ctx context.Context, s session.Session, text string) error { return nil }
func (t *menuTerminal) Reveal(ctx context.Context, s session.Session, activate bool) error {
	return nil
}
func (t *menuTerminal) Screen(ctx context.Context, s session.Session, lines int) (string, bool) {
	return t.Capture(ctx, s)
}
func (t *menuTerminal) Capture(ctx context.Context, s session.Session) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.blind {
		return "", false
	}
	return t.screen, true
}
func (t *menuTerminal) Keystroke(ctx context.Context, s session.Session, b []byte) error {
	t.mu.Lock()
	t.typed = append(t.typed, string(b))
	hook := t.onKey
	t.mu.Unlock()
	if hook != nil {
		hook(t, b)
	}
	return nil
}
func (t *menuTerminal) show(screen string) {
	t.mu.Lock()
	t.screen = screen
	t.mu.Unlock()
}
func (t *menuTerminal) keys() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.typed...)
}

func menuActions(t *menuTerminal) Actions {
	return Actions{Inventory: Inventory{Terminals: []ports.TerminalHost{t}, Screen: t},
		Terminals: []ports.TerminalHost{t}, Lanes: lane.New(lane.DefaultLimit)}
}

func seenMenu(t *testing.T, screen string) session.Menu {
	t.Helper()
	m, ok := session.ReadMenu(screen, session.AssistantClaude, true)
	if !ok {
		t.Fatalf("the fixture is not a menu:\n%s", screen)
	}
	return m
}

func paneFor(id string) session.Session {
	return session.Session{ID: id, Backend: session.BackendTmux, Assistant: session.AssistantClaude,
		State: session.StateWaiting}
}

// F1: the page read the prompt for `rm -rf build`; by the time its "1" lands,
// that one was answered and the prompt for `rm -rf /` is up. Nothing is typed,
// and the refusal says why in a code of its own.
func TestAnAnswerForAQuestionThatMovedOnTypesNothing(t *testing.T) {
	pane := &menuTerminal{s: paneFor("%7"), screen: permissionPrompt("rm -rf /", 1)}
	expect := session.MenuFingerprint(seenMenu(t, permissionPrompt("rm -rf build", 1)))
	_, err := menuActions(pane).Key(context.Background(), "%7", "1", expect)
	ref, ok := err.(Refusal)
	if !ok || ref.Code != "menu_moved" {
		t.Fatalf("err %v, want a menu_moved refusal", err)
	}
	if typed := pane.keys(); len(typed) != 0 {
		t.Fatalf("typed %q at a question nobody saw", typed)
	}
}

// The same when the question has gone altogether: a digit with no picker to
// take it falls into the composer as the start of the next message.
func TestAnAnswerWithNoQuestionOnScreenTypesNothing(t *testing.T) {
	pane := &menuTerminal{s: paneFor("%7"), screen: "✻ Working… (3s · esc to interrupt)\n\n> \n"}
	expect := session.MenuFingerprint(seenMenu(t, permissionPrompt("rm -rf build", 1)))
	_, err := menuActions(pane).Key(context.Background(), "%7", "1", expect)
	if ref, ok := err.(Refusal); !ok || ref.Code != "menu_moved" {
		t.Fatalf("err %v, want menu_moved", err)
	}
	if typed := pane.keys(); len(typed) != 0 {
		t.Fatalf("typed %q with no question up", typed)
	}
}

// A screen that cannot be read is not evidence the question is still there.
// Without an expectation the old rule stands — a capture that fails takes the
// numbered path — but an answer that named its question is refused unread.
func TestAnAnswerThatNamedItsQuestionIsNotTypedBlind(t *testing.T) {
	pane := &menuTerminal{s: paneFor("%7"), screen: permissionPrompt("rm -rf build", 1), blind: true}
	expect := session.MenuFingerprint(seenMenu(t, permissionPrompt("rm -rf build", 1)))
	_, err := menuActions(pane).Key(context.Background(), "%7", "1", expect)
	if ref, ok := err.(Refusal); !ok || ref.Code != "menu_unreadable" {
		t.Fatalf("err %v, want menu_unreadable", err)
	}
	if typed := pane.keys(); len(typed) != 0 {
		t.Fatalf("typed %q at a screen nobody could read", typed)
	}
}

// The question the page named is the one up: the digit goes, the screen shows
// it landed on that row, and Return commits it — the ordinary answer.
func TestAnAnswerForTheQuestionOnScreenIsTyped(t *testing.T) {
	pane := &menuTerminal{s: paneFor("%7"), screen: permissionPrompt("rm -rf build", 1)}
	pane.onKey = func(p *menuTerminal, b []byte) {
		if string(b) == "2" {
			p.show(permissionPrompt("rm -rf build", 2))
		}
	}
	expect := session.MenuFingerprint(seenMenu(t, permissionPrompt("rm -rf build", 1)))
	if _, err := menuActions(pane).Key(context.Background(), "%7", "2", expect); err != nil {
		t.Fatalf("err %v", err)
	}
	if typed := pane.keys(); len(typed) != 2 || typed[0] != "2" || typed[1] != "\r" {
		t.Fatalf("typed %q, want the digit then Return", typed)
	}
}

// The caret is not part of the question: the page read the prompt with the
// caret on row 1, somebody moved it to row 3, and the answer still goes.
func TestTheCaretMovingIsNotTheQuestionMoving(t *testing.T) {
	pane := &menuTerminal{s: paneFor("%7"), screen: permissionPrompt("rm -rf build", 3)}
	expect := session.MenuFingerprint(seenMenu(t, permissionPrompt("rm -rf build", 1)))
	if _, err := menuActions(pane).Key(context.Background(), "%7", "3", expect); err != nil {
		t.Fatalf("err %v", err)
	}
	if typed := pane.keys(); len(typed) == 0 || typed[0] != "3" {
		t.Fatalf("typed %q", typed)
	}
}

// An expectation that is not a fingerprint is refused before a session is
// looked up, like a key that is not on the list.
func TestAMalformedExpectationIsABadRequest(t *testing.T) {
	_, err := Actions{}.Key(context.Background(), "%1", "1", "not a fingerprint")
	if ref, ok := err.(Refusal); !ok || ref.Code != "bad_request" {
		t.Fatalf("err %v", err)
	}
}
