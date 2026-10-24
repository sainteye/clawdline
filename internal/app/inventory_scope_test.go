package app

import (
	"context"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	"testing"
)

type managedTestTerminal struct {
	ports.TerminalHost
	normal, scoped int
}

func (h *managedTestTerminal) Name() string { return "tmux" }
func (h *managedTestTerminal) Inventory(context.Context) (session.Inventory, error) {
	h.normal++
	return session.Inventory{Provenance: "tmux", Complete: true, Sessions: []session.Session{
		{ID: "%1", TTY: "ttys001", Assistant: session.AssistantClaude},
		{ID: "%2", TTY: "ttys002", Assistant: session.AssistantClaude},
	}}, nil
}
func (h *managedTestTerminal) ManagedInventory(context.Context) (session.Inventory, error) {
	h.scoped++
	return session.Inventory{Provenance: "tmux", Complete: true, Sessions: []session.Session{
		{ID: "%1", TTY: "ttys001", Assistant: session.AssistantClaude},
		{ID: "%2", TTY: "ttys002", DiscoveryExcluded: true},
	}}, nil
}

type managedTestProcess struct {
	normal   int
	selected map[string]bool
}

func (p *managedTestProcess) Scan(context.Context) (session.Inventory, error) {
	p.normal++
	return session.Inventory{Provenance: "ps", Complete: true}, nil
}
func (p *managedTestProcess) ScanTTYs(_ context.Context, selected map[string]bool) (session.Inventory, error) {
	p.selected = selected
	return session.Inventory{Provenance: "ps", Complete: true}, nil
}

type managedTestScreens struct{ ids []string }

func (s *managedTestScreens) Capture(_ context.Context, row session.Session) (string, bool) {
	s.ids = append(s.ids, row.ID)
	return "", false
}

func TestDiscoveryOffReadsOnlyManagedConversationsAndPreservesExternalPresence(t *testing.T) {
	terminal := &managedTestTerminal{}
	process := &managedTestProcess{}
	screen := &managedTestScreens{}
	on := false
	in := Inventory{DiscoverExternal: func() bool { return on }, Terminals: []ports.TerminalHost{terminal}, Process: process, Screen: screen}
	got := in.Read(context.Background())
	if process.normal != 0 || !process.selected["ttys001"] || process.selected["ttys002"] {
		t.Fatalf("read external process records: %+v", process)
	}
	if len(screen.ids) != 1 || screen.ids[0] != "%1" {
		t.Fatalf("screen reads: %v", screen.ids)
	}
	if !got.CompleteApartFromDisabled() || len(got.Sessions) != 2 {
		t.Fatalf("scoped inventory: %+v", got)
	}
	for _, row := range got.Sessions {
		if row.ID == "%2" && (!row.DiscoveryExcluded || row.IsAssistant()) {
			t.Fatalf("external row exposed: %+v", row)
		}
	}
	on = true
	screen.ids = nil
	in.Read(context.Background())
	if terminal.normal != 1 || terminal.scoped != 1 || process.normal != 1 || len(screen.ids) != 2 {
		t.Fatalf("discovery did not resume: terminal=%+v process=%+v screens=%v", terminal, process, screen.ids)
	}
}
