package app

import (
	"context"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// composer is Claude Code's input line holding line, framed by its rules.
func composer(line string) string {
	const rule = "────────────────────────────────────────"
	return strings.Join([]string{"⏺ Done.", "", rule, "❯ " + line, rule, "  ? for shortcuts"}, "\n")
}

// A send the terminal took and never submitted is in the composer, and an
// Enter from the phone submits it: one Return, nothing else.
func TestAnEnterAtTheWordsStillInTheComposerSubmitsThem(t *testing.T) {
	pane := &menuTerminal{s: paneFor("%7"), screen: composer("please read CHILD.md")}
	if _, err := menuActions(pane).SubmitTyped(context.Background(), "%7", "please read CHILD.md"); err != nil {
		t.Fatalf("err %v", err)
	}
	if typed := pane.keys(); len(typed) != 1 || typed[0] != "\r" {
		t.Fatalf("typed %q, want one Return", typed)
	}
}

// Everywhere else nothing is pressed, and the refusal says so in a code the
// page reads as "not done".
func TestAnEnterAnywhereElsePressesNothing(t *testing.T) {
	cases := []struct {
		name, screen string
		blind        bool
		code         string
	}{
		{"the words were submitted or cleared", composer(""), false, "input_moved"},
		{"someone typed something else", composer("git push --force"), false, "input_moved"},
		{"a question is up", permissionPrompt("rm -rf /", 1), false, "input_behind_question"},
		{"the assistant is gone and a shell has the words", "❯ please read CHILD.md", false, "input_moved"},
		{"the screen cannot be read", "", true, "input_unreadable"},
	}
	for _, c := range cases {
		pane := &menuTerminal{s: paneFor("%7"), screen: c.screen, blind: c.blind}
		_, err := menuActions(pane).SubmitTyped(context.Background(), "%7", "please read CHILD.md")
		if ref, ok := err.(Refusal); !ok || ref.Code != c.code {
			t.Errorf("%s: err %v, want %s", c.name, err, c.code)
		}
		if typed := pane.keys(); len(typed) != 0 {
			t.Errorf("%s: typed %q", c.name, typed)
		}
	}
}

// inputTerminal is a pane whose host can also read the input line the way a
// send does, with faint text left out.
type inputTerminal struct {
	*menuTerminal
	input string
}

func (t *inputTerminal) InputScreen(ctx context.Context, s session.Session) (string, bool) {
	return t.input, true
}

// An Enter is not pressed at a faint prompt suggestion that happens to be the
// same words. The phone's "press Enter" card can outlive the words it was for:
// they were submitted or cleared on the machine, and Claude Code then drew the
// same line again faint as its next suggestion. Read without styling, that
// composer held the words, and the Enter went to an empty composer.
func TestAnEnterIsNotPressedAtAFaintSuggestionOfTheSameWords(t *testing.T) {
	words := "commit、push、deploy。"
	pane := &inputTerminal{menuTerminal: &menuTerminal{s: paneFor("%7"), screen: composer(words)}, input: composer("")}
	actions := Actions{Inventory: Inventory{Terminals: []ports.TerminalHost{pane}, Screen: pane},
		Terminals: []ports.TerminalHost{pane}, Lanes: lane.New(lane.DefaultLimit)}
	_, err := actions.SubmitTyped(context.Background(), "%7", words)
	if ref, ok := err.(Refusal); !ok || ref.Code != "input_moved" {
		t.Fatalf("err %v, want input_moved", err)
	}
	if typed := pane.keys(); len(typed) != 0 {
		t.Fatalf("typed %q at a faint suggestion", typed)
	}
	pane.input = composer(words)
	if _, err := actions.SubmitTyped(context.Background(), "%7", words); err != nil {
		t.Fatalf("words really typed: err %v", err)
	}
	if typed := pane.keys(); len(typed) != 1 || typed[0] != "\r" {
		t.Fatalf("typed %q, want one Return", typed)
	}
}
