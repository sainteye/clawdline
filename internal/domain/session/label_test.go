package session

import "testing"

// The order is the Swift app's `TargetSession.preferredDisplayLabel`.

func TestPreferredLabelTakesTheFirstRungWithSomethingOnIt(t *testing.T) {
	full := LabelRungs{Manual: "typed", Orchestrator: "task", Conversation: "self", Thread: "thread", Handle: "clawdline-9d", Coordinate: "⌘1-2"}
	if got := PreferredLabel(full); got != "typed" {
		t.Fatalf("got %q", got)
	}
	if got := PreferredLabel(LabelRungs{Orchestrator: "  ", Conversation: "self", Handle: "h"}); got != "self" {
		t.Fatalf("a blank rung must be skipped, got %q", got)
	}
	if got := PreferredLabel(LabelRungs{Handle: "clawdline-9d", Coordinate: "⌘1-2"}); got != "clawdline-9d" {
		t.Fatalf("got %q", got)
	}
	if got := PreferredLabel(LabelRungs{Coordinate: "⌘1-2"}); got != "⌘1-2" {
		t.Fatalf("got %q", got)
	}
}

func TestAWeakTitleStepsAsideOnlyForAFallback(t *testing.T) {
	if got := DisplayedConversationTitle("Image #1", true, "Fix the parser"); got != "" {
		t.Fatalf("got %q, want the title to give way", got)
	}
	if got := DisplayedConversationTitle("Image #1", true, " "); got != "Image #1" {
		t.Fatalf("got %q, want the title kept with nothing under it", got)
	}
	if got := DisplayedConversationTitle("Fix it", false, "other"); got != "Fix it" {
		t.Fatalf("got %q", got)
	}
}
