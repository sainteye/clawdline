package app

import (
	"context"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// An assistant in an iTerm2 tab is listed by the tab's session id, the name a
// start answers with and the iTerm2 adapter acts on; a tmux pane keeps its id.
func TestMergeNamesAnITermTabByItsSessionID(t *testing.T) {
	process := session.Session{ID: "ttys017", TTY: "ttys017", Backend: session.BackendITerm,
		Assistant: session.AssistantClaude, PID: 7}
	tab := session.Session{ID: "D3790000-0000-4000-8000-000000000017", TTY: "ttys017", Backend: session.BackendITerm}
	got := richer(process, tab)
	if got.ID != tab.ID || got.PID != 7 || got.Assistant != session.AssistantClaude {
		t.Fatalf("got %+v", got)
	}
	pane := session.Session{ID: "%12", TTY: "ttys003", Backend: session.BackendTmux}
	if got := richer(richer(process, pane), tab); got.ID != "%12" {
		t.Fatalf("a pane id must survive: %+v", got)
	}
}

type titleJoinIdentity struct{ saw string }

func (i *titleJoinIdentity) ForSession(_ context.Context, s session.Session) (ports.Identity, bool) {
	i.saw = s.Label
	return ports.Identity{}, false
}

// A terminal title is offered to the identity adapter as a join key, then
// discarded. It never becomes the row's displayed conversation name merely
// because the terminal said it.
func TestEnrichOffersButDoesNotPublishTheTerminalTitle(t *testing.T) {
	id := &titleJoinIdentity{}
	in := Inventory{Identity: id}
	got := in.enrich(context.Background(), session.Session{
		ID: "tab", TTY: "ttys005", Backend: session.BackendITerm,
		Assistant: session.AssistantCodex, Label: "terminal-controlled title",
	})
	if id.saw != "terminal-controlled title" {
		t.Fatalf("identity source saw %q", id.saw)
	}
	if got.Label == "terminal-controlled title" {
		t.Fatal("the terminal title became the displayed identity")
	}
}
