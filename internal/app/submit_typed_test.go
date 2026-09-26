package app

import (
	"context"
	"strings"
	"testing"
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
