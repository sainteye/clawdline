package terminal

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeInput is a session whose screen is scripted look by look, and which
// records every paste, Enter and look in the order they happened.
type fakeInput struct {
	screens  []string // what each look answers; the last one repeats
	pasteErr error
	events   []string
	looks    int
}

func (f *fakeInput) paste(ctx context.Context, text string) error {
	if f.pasteErr != nil {
		return f.pasteErr
	}
	f.events = append(f.events, "paste")
	return nil
}

func (f *fakeInput) enter(ctx context.Context) error {
	f.events = append(f.events, "enter")
	return nil
}

func (f *fakeInput) screen(ctx context.Context) (string, bool) {
	i := min(f.looks, len(f.screens)-1)
	f.looks++
	f.events = append(f.events, "look:"+f.screens[i])
	return f.screens[i], true
}

const rule = "────────────────────────────────────────"

func composerScreen(line string) string {
	return strings.Join([]string{"❯ earlier message", "", rule, "❯ " + line, rule, "  ? for shortcuts"}, "\n")
}

// fastSubmit shortens every wait, so a test that waits out a window is quick.
func fastSubmit(t *testing.T) {
	t.Helper()
	pauses, nudges := submitPauses, nudgePauses
	t.Cleanup(func() { submitPauses, nudgePauses = pauses, nudges })
	submitPauses = []time.Duration{time.Millisecond}
	nudgePauses = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
}

// The Enter comes after the first look that shows the paste, never before,
// however many looks that takes.
func TestSubmitPressesEnterOnlyAfterTheScreenShowsThePaste(t *testing.T) {
	fastSubmit(t)
	text := "please read the briefing 0123456789abcdef0123456789abcdef"
	empty, shown, sent := composerScreen(""), composerScreen(text), composerScreen("")
	in := &fakeInput{screens: []string{empty, empty, empty, empty, shown, sent}}
	if err := submit(context.Background(), in, text, time.Second); err != nil {
		t.Fatal(err)
	}
	enter := -1
	for i, e := range in.events {
		if e == "enter" {
			if enter >= 0 {
				t.Fatalf("a second Enter into a composer that let go: %v", in.events)
			}
			enter = i
		}
	}
	if enter < 0 {
		t.Fatalf("no Enter at all: %v", in.events)
	}
	if in.events[enter-1] != "look:"+shown {
		t.Fatalf("the Enter did not follow the look that showed the paste: %v", in.events)
	}
	for _, e := range in.events[:enter-1] {
		if e == "look:"+shown {
			t.Fatalf("the paste was shown earlier and Enter waited: %v", in.events)
		}
	}
}

// A screen that never shows the paste gets no Enter, and the answer is
// Unsubmitted — not Unsent, because the text went in.
func TestSubmitThatNeverSeesThePasteIsUnsubmitted(t *testing.T) {
	fastSubmit(t)
	in := &fakeInput{screens: []string{composerScreen("")}}
	err := submit(context.Background(), in, "a line nobody reads", 30*time.Millisecond)
	var unsubmitted Unsubmitted
	var unsent Unsent
	if !errors.As(err, &unsubmitted) || errors.As(err, &unsent) {
		t.Fatalf("answered %T %v, want Unsubmitted", err, err)
	}
	for _, e := range in.events {
		if e == "enter" {
			t.Fatalf("an Enter reached a screen that never showed the paste: %v", in.events)
		}
	}
}

// A paste refused before its first byte is Unsent, and nothing follows it.
func TestSubmitWhosePasteWasRefusedTypesNothingMore(t *testing.T) {
	fastSubmit(t)
	in := &fakeInput{screens: []string{composerScreen("")}, pasteErr: Unsent{Why: "can't find pane: %9"}}
	err := submit(context.Background(), in, "hello", time.Second)
	var unsent Unsent
	if !errors.As(err, &unsent) {
		t.Fatalf("answered %T %v, want Unsent", err, err)
	}
	for _, e := range in.events {
		if e == "enter" {
			t.Fatalf("an Enter after a refused paste: %v", in.events)
		}
	}
}

// A framed composer that still shows exactly what was confirmed after the
// Enter gets another; a shell whose line stays on screen as the command it
// ran does not — that Enter would go to the command.
func TestSubmitNudgesOnlyAFramedComposerThatKeptThePaste(t *testing.T) {
	fastSubmit(t)
	text := "please read the briefing 0123456789abcdef0123456789abcdef"
	kept := composerScreen(text)
	in := &fakeInput{screens: []string{composerScreen(""), kept, kept, composerScreen("")}}
	if err := submit(context.Background(), in, text, time.Second); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(in.events, "|"), "enter"); n != 2 {
		t.Fatalf("a composer that kept the paste once got %d Enter(s): %v", n, in.events)
	}

	command := "claude --permission-mode bypassPermissions"
	shell := &fakeInput{screens: []string{"❯ ", "❯ " + command}}
	if err := submit(context.Background(), shell, command, time.Second); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(strings.Join(shell.events, "|"), "enter"); n != 1 {
		t.Fatalf("a shell line got %d Enter(s): %v", n, shell.events)
	}
}

// ruleCase is one screen pair the rule is asked about, in Go and in
// JavaScript alike.
type ruleCase struct {
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
	Text   string `json:"text"`
	Shows  bool   `json:"shows"`
	Holds  bool   `json:"holds"`
}

func ruleCases() []ruleCase {
	notice := `<clawdline-notice>{"body":"` + strings.Repeat("x", 900) +
		`","ack_path":"/v1/orchestrator/tasks/0000/completion/ack"}</clawdline-notice>`
	return []ruleCase{
		{Name: "claude shows a long paste as a placeholder", Before: composerScreen(""),
			After: composerScreen("[Pasted text #5]"), Text: notice, Shows: true, Holds: true},
		{Name: "codex shows a long paste as a placeholder",
			Before: "› \n\n  ⏎ send", After: "› [Pasted Content 1045 chars]\n\n  ⏎ send", Text: notice, Shows: true},
		{Name: "codex turns a pasted image path into an attachment",
			Before: "› \n\n  ⏎ send", After: "› [Image #1]\n\n  ⏎ send",
			Text: "/tmp/clawdline-20260923-155101-000-example.png", Shows: true},
		{Name: "a placeholder already there is not this paste", Before: composerScreen("[Pasted text #4]"),
			After: composerScreen("[Pasted text #4]"), Text: notice, Holds: true},
		{Name: "a short line shows itself", Before: composerScreen(""), After: composerScreen("y"), Text: "y",
			Shows: true, Holds: true},
		{Name: "a wrapped line still matches", Before: composerScreen(""),
			After: composerScreen("please read CHILD.md 0123456789abcdef\n  0123456789abcdef"),
			Text:  "please read CHILD.md 0123456789abcdef0123456789abcdef", Shows: true, Holds: true},
		{Name: "the same message above the composer is not this paste",
			Before: strings.Join([]string{"❯ the same line again 0123456789abcdef", rule, "❯ ", rule}, "\n"),
			After:  strings.Join([]string{"❯ the same line again 0123456789abcdef", rule, "❯ ", rule}, "\n"),
			Text:   "the same line again 0123456789abcdef"},
		{Name: "a shell echoes the command", Before: "user@mac ~ % ",
			After: "user@mac ~ % claude --add-dir /tmp/x", Text: "claude --add-dir /tmp/x", Shows: true},
		{Name: "a shell line is never nudged", Before: "❯ ", After: "❯ claude --add-dir /tmp/x",
			Text: "claude --add-dir /tmp/x", Shows: true},
		{Name: "a chooser is not a composer",
			Before: "❯ No, exit\n  Yes, I trust this folder\nEnter to confirm",
			After:  "❯ No, exit\n  Yes, I trust this folder\nEnter to confirm", Text: "please read CHILD.md"},
	}
}

// The rule's own cases, in Go.
func TestTheInputRuleReadsTheScreensItWasWrittenFor(t *testing.T) {
	for _, c := range ruleCases() {
		if got := showsText(c.Before, c.After, c.Text); got != c.Shows {
			t.Errorf("%s: showsText = %v, want %v", c.Name, got, c.Shows)
		}
		if got := stillHolds(c.After, c.After); got != c.Holds {
			t.Errorf("%s: stillHolds = %v, want %v", c.Name, got, c.Holds)
		}
	}
}

// iTerm2's script reads the screen with inputRuleJS, tmux with the Go. The
// two must answer the same, or the two backends are not one behaviour.
func TestTheInputRuleIsTheSameInGoAndJavaScript(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the JavaScript rule is run in node")
	}
	cases, _ := json.Marshal(ruleCases())
	harness := inputRuleJS + `
const cases = JSON.parse(require("fs").readFileSync(0, "utf8"));
process.stdout.write(JSON.stringify(cases.map(c => ({
  shows: showsText(c.before, c.after, c.text), holds: stillHolds(c.after, c.after),
}))));
`
	path := filepath.Join(t.TempDir(), "rule.js")
	if err := os.WriteFile(path, []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, path)
	cmd.Stdin = strings.NewReader(string(cases))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the JavaScript rule threw: %v\n%s", err, out)
	}
	var answers []struct{ Shows, Holds bool }
	if err := json.Unmarshal(out, &answers); err != nil {
		t.Fatalf("unreadable answer %q: %v", out, err)
	}
	for i, c := range ruleCases() {
		if answers[i].Shows != showsText(c.Before, c.After, c.Text) || answers[i].Holds != stillHolds(c.After, c.After) {
			t.Errorf("%s: JavaScript answered %+v, Go shows=%v holds=%v", c.Name, answers[i],
				showsText(c.Before, c.After, c.Text), stillHolds(c.After, c.After))
		}
	}
}

// The iTerm2 script is given as many looks as the tmux send makes in the
// same window.
func TestLooksWithinCountsTheLooksAWindowHolds(t *testing.T) {
	if got := looksWithin(0); got != 1 {
		t.Fatalf("a window of nothing holds %d looks, want the first", got)
	}
	var spent time.Duration
	for i := 0; i < looksWithin(sendConfirm)-1; i++ {
		spent += submitPauses[min(i, len(submitPauses)-1)]
	}
	if spent > sendConfirm || spent+submitPauses[len(submitPauses)-1] <= sendConfirm {
		t.Fatalf("%d looks span %s of a %s window", looksWithin(sendConfirm), spent, sendConfirm)
	}
}
