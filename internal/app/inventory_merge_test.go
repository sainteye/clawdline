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

type observingIdentity struct {
	titleJoinIdentity
	observed []string
	askedAt  []int
}

func (i *observingIdentity) ObserveRows(ctx context.Context, rows []session.Session) context.Context {
	for _, s := range rows {
		i.observed = append(i.observed, s.Label)
	}
	return ctx
}

func (i *observingIdentity) ForSession(ctx context.Context, s session.Session) (ports.Identity, bool) {
	i.askedAt = append(i.askedAt, len(i.observed))
	return i.titleJoinIdentity.ForSession(ctx, s)
}

type twoTabsHost struct{ *overlapHost }

func (h *twoTabsHost) Inventory(context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Provenance: "iterm", Sessions: []session.Session{
		{ID: "A", TTY: "tty1", Backend: session.BackendITerm, Assistant: session.AssistantCodex, Label: "demo (codex)"},
		{ID: "B", TTY: "tty2", Backend: session.BackendITerm, Assistant: session.AssistantCodex, Label: "demo (codex)"},
	}}, nil
}

// An identity source that asks to see every row is shown all of them, with
// their terminal titles, before it is asked to name any one: two tabs with one
// bare Codex title name neither, and only the whole reading can tell.
func TestReadShowsEveryRowBeforeAskingForAnIdentity(t *testing.T) {
	id := &observingIdentity{}
	host := &twoTabsHost{overlapHost: newOverlapHost("%1")}
	Inventory{Identity: id, Terminals: []ports.TerminalHost{host}}.Read(context.Background())
	if len(id.observed) != 2 || id.observed[0] != "demo (codex)" || id.observed[1] != "demo (codex)" {
		t.Fatalf("observed %q; want both terminal titles", id.observed)
	}
	if len(id.askedAt) != 2 || id.askedAt[0] != 2 || id.askedAt[1] != 2 {
		t.Fatalf("identities asked after %v observed rows; want every row first", id.askedAt)
	}
}
