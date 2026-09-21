package http

import (
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/swiftstore"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// A Codex session that nobody has typed into yet is on this list like any
// other, and the list says so in words rather than by leaving the row blank.
// The row somebody opened a moment ago is exactly the row they are looking
// for, and an empty one reads as a session that is not there.
func TestAFreshCodexIsOnTheWireWithBothAPlaceAndAReason(t *testing.T) {
	s := &Server{}
	fresh := session.Session{
		ID: "ttys039", TTY: "ttys039", PID: 74653,
		Backend: session.BackendITerm, Assistant: session.AssistantCodex,
		State: session.StateUnknown, Evidence: session.EvidenceProcess,
		Binding: session.BindingNoRecord,
		Rungs:   session.LabelRungs{Coordinate: "Codex · ttys039"},
	}
	row := wire(t, s.sessionRow(rowInput{item: fresh, label: "Codex · ttys039"}).SessionRow)
	if row["label"] != "Codex · ttys039" {
		t.Fatalf("label = %v", row["label"])
	}
	if row["identity"] != "no_record" {
		t.Fatalf("identity = %v, want the reason it has no id", row["identity"])
	}
	if _, ok := row["sessionId"]; ok {
		t.Fatalf("sessionId = %v, want no id invented for it", row["sessionId"])
	}

	// The reading failing is a different answer from the session not having
	// written anything, and the wire keeps them apart.
	fresh.Binding = session.BindingUnreadable
	if got := wire(t, s.sessionRow(rowInput{item: fresh}).SessionRow)["identity"]; got != "unreadable" {
		t.Fatalf("identity = %v", got)
	}

	// A row nothing was asked about carries no key at all, rather than a
	// value that would read as one of the six real answers.
	fresh.Binding = ""
	if _, ok := wire(t, s.sessionRow(rowInput{item: fresh}).SessionRow)["identity"]; ok {
		t.Fatal("identity is present on a row no source answered for")
	}
}

// A project is a name somebody can recognise; a tty is only where the
// terminal happens to be. It is the fallback immediately above the coordinate
// and never replaces a name the person, broker or conversation supplied.
func TestProjectNamesANewSessionBeforeItsTerminalCoordinate(t *testing.T) {
	item := session.Session{
		Assistant: session.AssistantCodex,
		Rungs:     session.LabelRungs{Coordinate: "Codex · ttys020"},
	}
	if got := rowLabel(item, swiftstore.Titles{}, "my-app"); got != "Codex · my-app" {
		t.Fatalf("label = %q", got)
	}
	if got := rowLabel(item, swiftstore.Titles{Manual: "Release helper"}, "my-app"); got != "Release helper" {
		t.Fatalf("manual label was replaced by %q", got)
	}
}

func wire(t *testing.T, v any) map[string]any {
	t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
