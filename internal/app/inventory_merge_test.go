package app

import (
	"testing"

	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// An assistant in an iTerm2 tab is listed by the tab's session id, the name a
// start answers with and the iTerm2 adapter acts on; a tmux pane keeps its id.
func TestMergeNamesAnITermTabByItsSessionID(t *testing.T) {
	process := session.Session{ID: "ttys017", TTY: "ttys017", Backend: session.BackendITerm,
		Assistant: session.AssistantClaude, PID: 7}
	tab := session.Session{ID: "D379E32C-9201-45F1-B889-25D015FB56B5", TTY: "ttys017", Backend: session.BackendITerm}
	got := richer(process, tab)
	if got.ID != tab.ID || got.PID != 7 || got.Assistant != session.AssistantClaude {
		t.Fatalf("got %+v", got)
	}
	pane := session.Session{ID: "%12", TTY: "ttys003", Backend: session.BackendTmux}
	if got := richer(richer(process, pane), tab); got.ID != "%12" {
		t.Fatalf("a pane id must survive: %+v", got)
	}
}
