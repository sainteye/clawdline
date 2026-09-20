package http

import (
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/session"
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
