package session

import "testing"

// A row with no name reads as a session that is not there, which is the one
// thing a list of what is running must never say about something running.
func TestCoordinateNamesASessionNothingElseNamed(t *testing.T) {
	fresh := Session{Assistant: AssistantCodex, TTY: "ttys039", ID: "ttys039"}
	if got := Coordinate(fresh); got != "Codex · ttys039" {
		t.Fatalf("got %q", got)
	}
	pane := Session{Assistant: AssistantClaude, ID: "%12"}
	if got := Coordinate(pane); got != "Claude · %12" {
		t.Fatalf("a session with no tty is named by where it is, got %q", got)
	}
	if got := Coordinate(Session{ID: "ttys040"}); got != "" {
		t.Fatalf("an ordinary shell is no assistant and gets no name, got %q", got)
	}
}

// It is the bottom rung: anything a person or a task called this session wins.
func TestCoordinateNeverDisplacesARealName(t *testing.T) {
	rungs := LabelRungs{Coordinate: Coordinate(Session{Assistant: AssistantCodex, TTY: "ttys039"})}
	if got := PreferredLabel(rungs); got != "Codex · ttys039" {
		t.Fatalf("got %q", got)
	}
	rungs.Orchestrator = "剛開的 Codex 認不出來"
	if got := PreferredLabel(rungs); got != "剛開的 Codex 認不出來" {
		t.Fatalf("the task's title must win, got %q", got)
	}
}

func TestOnlyABindingWithAnIdIsBound(t *testing.T) {
	for _, b := range []Binding{BindingCommandLine, BindingOpenFile, BindingRegistry} {
		if !b.Bound() {
			t.Fatalf("%q carries an id", b)
		}
	}
	for _, b := range []Binding{BindingNoRecord, BindingUnreadable, BindingAmbiguous, ""} {
		if b.Bound() {
			t.Fatalf("%q carries none", b)
		}
	}
}
