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

type terminalPaneIdentity struct{}

func (terminalPaneIdentity) ForSession(context.Context, session.Session) (ports.Identity, bool) {
	// Provider records know only the server-local TMUX_PANE, not its socket.
	return ports.Identity{Pane: "%0", ConversationID: "terminal-conversation"}, true
}

func TestTerminalPaneKeepsItsServerWhenEnrichedFromAProviderRecord(t *testing.T) {
	process := session.Session{ID: "ttys017", TTY: "ttys017", Backend: session.BackendITerm,
		Assistant: session.AssistantCodex, PID: 7}
	pane := session.Session{ID: "terminal:%0", TTY: process.TTY, Backend: session.BackendTmux}
	in := Inventory{Identity: terminalPaneIdentity{}}
	got := in.enrich(context.Background(), richer(process, pane))
	if got.ID != pane.ID || got.PID != 7 || got.Assistant != session.AssistantCodex || got.ConversationID != "terminal-conversation" {
		t.Fatalf("provider enrichment lost the Terminal server: %+v", got)
	}
}

func TestRemoteControlWorkerDoesNotBorrowItsLaunchersPane(t *testing.T) {
	in := Inventory{Identity: terminalPaneIdentity{}}
	row := session.Session{ID: "claude-remote:42:fixture", Backend: session.BackendClaudeRemote,
		Assistant: session.AssistantClaude, PID: 42, ConversationID: "fixture"}
	got := in.enrich(context.Background(), row)
	if got.ID != row.ID || got.Backend != session.BackendClaudeRemote {
		t.Fatalf("worker became its launcher: %+v", got)
	}
}

func TestRemoteControlAndInteractiveResumeAreOneConversation(t *testing.T) {
	remote := session.Session{ID: "claude-remote:42:fixture", Backend: session.BackendClaudeRemote,
		Assistant: session.AssistantClaude, PID: 42, ConversationID: "fixture"}
	terminal := session.Session{ID: "%2", TTY: "ttys002", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, PID: 43, ConversationID: "fixture"}
	in := Inventory{Process: stubProcess{inv: session.Inventory{Sessions: []session.Session{remote, terminal}, Complete: true, Provenance: "ps"}}}
	got := in.Read(context.Background())
	if len(got.Sessions) != 1 || got.Sessions[0].ID != terminal.ID {
		t.Fatalf("same conversation was listed twice: %+v", got.Sessions)
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

func TestInventoryPublishesVerifiedSquadRolesForBothProviders(t *testing.T) {
	for _, provider := range []session.Assistant{session.AssistantCodex, session.AssistantClaude} {
		t.Run(string(provider), func(t *testing.T) {
			row := session.Session{ID: "%1", TTY: "tty1", Assistant: provider, SquadLaunchID: "launch"}
			calls := 0
			in := Inventory{
				Process: stubProcess{inv: session.Inventory{Sessions: []session.Session{row}, Complete: true}},
				SquadRole: func(_ context.Context, observed session.Session) string {
					calls++
					if observed.ID != row.ID || observed.SquadLaunchID != row.SquadLaunchID {
						t.Fatalf("wrong execution: %+v", observed)
					}
					return "personal.persona.editor"
				},
			}
			got := in.Read(context.Background())
			if len(got.Sessions) != 1 || got.Sessions[0].Persona != "personal.persona.editor" || calls != 1 {
				t.Fatalf("inventory=%+v calls=%d", got, calls)
			}
			row.SquadLaunchID = ""
			in.Process = stubProcess{inv: session.Inventory{Sessions: []session.Session{row}, Complete: true}}
			got = in.Read(context.Background())
			if len(got.Sessions) != 1 || got.Sessions[0].Persona != "" || calls != 1 {
				t.Fatalf("replacement inherited role: %+v calls=%d", got, calls)
			}
		})
	}
}
