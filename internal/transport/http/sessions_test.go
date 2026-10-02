package http

import (
	"encoding/json"
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

// rowJSON is a whole row as it is sent, the shadowed menu included.
func rowJSON(t *testing.T, row sessionRowWire) map[string]any {
	t.Helper()
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// A waiting row says whether this reading had its screen, so the card can tell
// a terminal that is not answering from a dialog nothing recognised; and a
// menu put together from the transcript says so, without a caret.
func TestAWaitingRowSaysWhetherItsScreenWasRead(t *testing.T) {
	s := &Server{}
	item := session.Session{ID: "w0t0p0", Backend: session.BackendITerm, Assistant: session.AssistantClaude,
		State: session.StateWaiting, Evidence: session.EvidenceRegistry, Screen: session.ScreenUnavailable}
	row := rowJSON(t, s.sessionRow(rowInput{item: item}))
	if row["screen_reading"] != "unavailable" {
		t.Fatalf("screen_reading = %v", row["screen_reading"])
	}
	if _, ok := row["menu"]; ok {
		t.Fatalf("menu = %v on a row without one", row["menu"])
	}

	asked := []session.AskedQuestion{{Text: "q", Options: []session.AskedOption{{Label: "a"}, {Label: "b"}}}}
	menu, _ := session.MenuFromTranscript(asked)
	item.Menu = &menu
	row = rowJSON(t, s.sessionRow(rowInput{item: item}))
	m, _ := row["menu"].(map[string]any)
	if m == nil || m["source"] != "transcript" || m["selected"] != nil || len(m["options"].([]any)) != 4 {
		t.Fatalf("menu = %v", row["menu"])
	}

	item.Menu, item.Screen = nil, session.ScreenRead
	if got := rowJSON(t, s.sessionRow(rowInput{item: item}))["screen_reading"]; got != "read" {
		t.Fatalf("screen_reading = %v, want read", got)
	}

	// Only a waiting row carries it: a working or idle row's screen comes and
	// goes with the terminal's health, and nothing on the card reads it there.
	item.State = session.StateWorking
	if got, ok := rowJSON(t, s.sessionRow(rowInput{item: item}))["screen_reading"]; ok {
		t.Fatalf("screen_reading = %v on a working row", got)
	}
}
