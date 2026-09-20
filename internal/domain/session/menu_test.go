package session

import (
	"os"
	"strings"
	"testing"
)

// The screens under testdata/ were captured with `tmux capture-pane -p -J` from
// a disposable Claude Code v2.1.274 session on 2026-09-17, while the Swift app
// (7717) was reading the same pane. Where a test states a whole menu, it is the
// menu that app published for that screen.
//
// `menu-ask-live`, `menu-composer-list` and `menu-echo-list` were captured the
// same way from Claude Code v2.1.278 on 2026-09-20, and `menu-codex-*` from
// codex-cli 0.155.1 the same day. Their words are this test's own; nothing here
// is anybody's message.

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func labels(m Menu) string {
	var out []string
	for _, o := range m.Options {
		out = append(out, o.Label)
	}
	return strings.Join(out, "|")
}

// AskUserQuestion draws its selected row flush left, which is also where the
// composer's caret is: only a registry saying "waiting" makes it a selection.
func TestAskUserQuestionNeedsTheGate(t *testing.T) {
	screen := fixture(t, "menu-ask-single.txt")
	if _, ok := ReadMenu(screen, AssistantClaude, false); ok {
		t.Fatal("a flush-left caret was read as a menu with the gate shut")
	}
	m, ok := ReadMenu(screen, AssistantClaude, true)
	if !ok {
		t.Fatal("no menu behind an open gate")
	}
	if got := labels(m); got != "Tea|Water|Coffee|Type something.|Chat about this" {
		t.Fatalf("labels %q", got)
	}
	if m.Question != "下午想喝什麼？" || m.Selected == nil || *m.Selected != 1 || !m.Numbered || m.Submit != nil {
		t.Fatalf("menu %+v", m)
	}
	if m.Options[1].Detail != "清爽解渴，清淨無味" || m.Options[3].Detail != "" {
		t.Fatalf("details %q %q", m.Options[1].Detail, m.Options[3].Detail)
	}
	if len(m.Steps) != 2 || m.Steps[0] != (MenuStep{Label: "飲料"}) || m.Steps[1] != (MenuStep{Label: "點心"}) {
		t.Fatalf("steps %+v", m.Steps)
	}
	// The Swift app's revision for this screen, as it published it in `line`.
	want := "下午想喝什麼？\x1e\x1e1\x1fTea\x1f1\x1e2\x1fWater\x1f0\x1e3\x1fCoffee\x1f0\x1e4\x1fType something.\x1f0\x1e5\x1fChat about this\x1f0\x1e飲料\x1f0\x1f\x1e點心\x1f0\x1f"
	if got := MenuRevision(m); got != want {
		t.Fatalf("revision %q", got)
	}
}

// A multi-select's rows tick, its descriptions sit left of the labels, and the
// line under them is the button — not the last row's description.
func TestMultiSelectReadsBoxesAndItsButton(t *testing.T) {
	m, ok := ReadMenu(fixture(t, "menu-ask-multi.txt"), AssistantClaude, true)
	if !ok {
		t.Fatal("no menu")
	}
	if got := labels(m); got != "Cookie|Cake|Fruit|Type something|Chat about this" {
		t.Fatalf("labels %q", got)
	}
	for i, o := range m.Options[:4] {
		if o.Checked == nil || *o.Checked {
			t.Fatalf("row %d checked %v", i, o.Checked)
		}
	}
	if m.Options[4].Checked != nil {
		t.Fatal("a row without a box reads as ticked or unticked")
	}
	if m.Options[3].Detail != "" || m.Options[0].Detail != "脆餅乾，香酥可口" {
		t.Fatalf("details %q %q", m.Options[0].Detail, m.Options[3].Detail)
	}
	if m.Submit == nil || m.Submit.Label != "Submit" || m.Submit.Selected {
		t.Fatalf("submit %+v", m.Submit)
	}
	if len(m.Steps) != 2 || !m.Steps[0].Answered || m.Steps[1].Answered {
		t.Fatalf("steps %+v", m.Steps)
	}
}

// The review screen names what was chosen, and the tab bar pairs it by position.
func TestReviewScreenPairsAnswersWithSteps(t *testing.T) {
	m, ok := ReadMenu(fixture(t, "menu-ask-review.txt"), AssistantClaude, true)
	if !ok {
		t.Fatal("no menu")
	}
	if got := labels(m); got != "Submit answers|Cancel" {
		t.Fatalf("labels %q", got)
	}
	want := []MenuStep{{Label: "飲料", Answered: true, Answer: "Water"}, {Label: "點心", Answered: true, Answer: "Cake, Fruit"}}
	if len(m.Steps) != 2 || m.Steps[0] != want[0] || m.Steps[1] != want[1] {
		t.Fatalf("steps %+v", m.Steps)
	}
}

// The trust dialog prints no numbers. It is read only behind the gate and
// numbered by position, because its digits do not select.
func TestUnnumberedPickerIsGatedAndCountedByPosition(t *testing.T) {
	screen := fixture(t, "menu-trust.txt")
	if _, ok := ReadMenu(screen, AssistantClaude, false); ok {
		t.Fatal("an unnumbered picker was read with the gate shut")
	}
	m, ok := ReadMenu(screen, AssistantClaude, true)
	if !ok {
		t.Fatal("no menu")
	}
	if m.Numbered || labels(m) != "No, exit|Yes, I trust this folder" || m.Selected == nil || *m.Selected != 1 {
		t.Fatalf("menu %+v", m)
	}
}

// Once answered, the transcript's own bullets and the empty composer are not a
// question, gate or no gate.
func TestAnsweredScreenIsNotAMenu(t *testing.T) {
	if m, ok := ReadMenu(fixture(t, "menu-answered.txt"), AssistantClaude, true); ok {
		t.Fatalf("read %+v", m)
	}
}

// A message that starts with a numbered list echoes as `❯ 1. …`: unframed, it
// is text even behind the gate.
func TestEchoedNumberedListIsNotAMenu(t *testing.T) {
	screen := "⏺ done\n\n❯ 1. first thing\n  2. second thing\n"
	if m, ok := ReadMenu(screen, AssistantClaude, true); ok {
		t.Fatalf("read %+v", m)
	}
}

// A digit that answered a question of a set puts the next one up; a Return
// then would land on a question nobody read.
func TestConfirmRefusesAReturnOnceThePickerMovedOn(t *testing.T) {
	single, _ := ReadMenu(fixture(t, "menu-ask-single.txt"), AssistantClaude, true)
	multi, _ := ReadMenu(fixture(t, "menu-ask-multi.txt"), AssistantClaude, true)
	if got := Confirm(2, &single, multi); got != ConfirmMovedOn {
		t.Fatalf("moved on read as %s", got)
	}
	if got := Confirm(1, &single, single); got != ConfirmSend {
		t.Fatalf("landed read as %s", got)
	}
	if got := Confirm(2, &single, single); got != ConfirmNotYet {
		t.Fatalf("not landed read as %s", got)
	}
	if got := Confirm(1, nil, single); got != ConfirmMovedOn {
		t.Fatalf("a set with nothing read before is %s", got)
	}
	if k, n := Walk(3, 1); k != 'k' || n != 2 {
		t.Fatalf("walk %c %d", k, n)
	}
}

// Words the pane clipped come from the call, but only for the question whose
// rows the screen proves it is showing.
func TestRefillOnlyTheQuestionOnScreen(t *testing.T) {
	m, _ := ReadMenu(fixture(t, "menu-ask-multi.txt"), AssistantClaude, true)
	m.Options[1].Label = "Ca…"
	asked := []AskedQuestion{
		{Text: "下午想喝什麼？", Options: []AskedOption{{Label: "Tea"}, {Label: "Water"}, {Label: "Coffee"}}},
		{Text: "要配哪些點心？", Options: []AskedOption{{Label: "Cookie", Note: "a"}, {Label: "Cake", Note: "b"}, {Label: "Fruit", Note: "c"}}},
	}
	got := RefillMenu(m, asked)
	if labels(got) != "Cookie|Cake|Fruit|Type something|Chat about this" || got.Options[1].Detail != "b" || got.Options[4].Detail != "" {
		t.Fatalf("refilled %+v", got.Options)
	}
	if m.Options[1].Label != "Ca…" {
		t.Fatal("refill wrote through to the menu it was given")
	}
	// Two questions with the same rows and no question on screen: nothing.
	twins := []AskedQuestion{asked[1], {Text: "再選一次？", Options: asked[1].Options}}
	m.Question = ""
	if got := RefillMenu(m, twins); got.Options[1].Label != "Ca…" {
		t.Fatal("an ambiguous screen was refilled")
	}
}

// **A message that began with a number is not a question.** Somebody reported
// two things, one to a line, and the console drew them as two buttons: the
// transcript echoes such a message as `\u276f 1. \u2026` with `2. \u2026` under it, which is
// the shape AskUserQuestion draws, and the frame the reading asks for was the
// one a markdown table printed a moment earlier. Below the rows is a turn still
// being written and the composer, and above them the previous turn's own line,
// which the card then showed as the question.
func TestAMessageThatBeganWithANumberIsNotAMenu(t *testing.T) {
	screen := fixture(t, "menu-echo-list.txt")
	for _, gate := range []bool{false, true} {
		if m, ok := ReadMenu(screen, AssistantClaude, gate); ok {
			t.Fatalf("gate %v read somebody's message as %+v", gate, m.Options)
		}
	}
}

// The composer holds whatever is typed into it, between two rules of its own.
// The capture is rejected twice over: Claude Code writes a no-break space after
// the caret it takes typing at, where every picker writes an ordinary one — and
// with that written as an ordinary space, which is all that stands between this
// screen and a menu, the frame closing straight onto the rows still is not a
// question being asked.
func TestTheComposerIsNotAMenu(t *testing.T) {
	screen := fixture(t, "menu-composer-list.txt")
	if m, ok := ReadMenu(screen, AssistantClaude, true); ok {
		t.Fatalf("the composer read as %+v", m.Options)
	}
	if m, ok := ReadMenu(strings.ReplaceAll(screen, "\u00a0", " "), AssistantClaude, true); ok {
		t.Fatalf("the composer read as %+v once its caret took an ordinary space", m.Options)
	}
}

// The control: a picker really on screen, captured from the version that drew
// the message above, is still read whole.
func TestALivePickerIsStillRead(t *testing.T) {
	m, ok := ReadMenu(fixture(t, "menu-ask-live.txt"), AssistantClaude, true)
	if !ok {
		t.Fatal("no menu on a screen with a picker on it")
	}
	if got := labels(m); got != "Tea|Water|Coffee|Type something.|Chat about this" {
		t.Fatalf("labels %q", got)
	}
	if m.Question != "What would you like this afternoon?" || m.Selected == nil || *m.Selected != 1 {
		t.Fatalf("menu %+v", m)
	}
	if m.Options[1].Detail != "Pure hydration, refreshing and essential." {
		t.Fatalf("detail %q", m.Options[1].Detail)
	}
	// One question of one is not a set, so the picker's `\u2610 Drink` bar names
	// no steps (stepsInLine).
	if len(m.Steps) != 0 {
		t.Fatalf("steps %+v", m.Steps)
	}
}

// Codex has no gate in front of this reading at all, so its composer was the
// cheaper accident: type a numbered list into it and the row went to a phone
// with buttons under it. What Codex draws over a picker's rows is the question
// it is asking; over the composer, two blank rows and the transcript.
func TestCodexComposerIsNotAMenu(t *testing.T) {
	screen := fixture(t, "menu-codex-composer.txt")
	for _, gate := range []bool{false, true} {
		if m, ok := ReadMenu(screen, AssistantCodex, gate); ok {
			t.Fatalf("gate %v read the Codex composer as %+v", gate, m.Options)
		}
	}
}

// The control on that side: Codex's own picker, captured from the same build.
func TestCodexPickerIsStillRead(t *testing.T) {
	m, ok := ReadMenu(fixture(t, "menu-codex-trust.txt"), AssistantCodex, false)
	if !ok {
		t.Fatal("no menu on a screen with a Codex picker on it")
	}
	if got := labels(m); got != "Yes, continue|No, quit" {
		t.Fatalf("labels %q", got)
	}
	if m.Selected == nil || *m.Selected != 1 || !m.Numbered {
		t.Fatalf("menu %+v", m)
	}
}
