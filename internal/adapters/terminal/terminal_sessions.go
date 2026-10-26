package terminal

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// ConnectTerminalServer shares the ordinary Terminal's fixed socket with session
// discovery and actions. No server is started, and no arbitrary socket is scanned.
func ConnectTerminalServer(hosts []ports.TerminalHost, stateDir string) {
	choice := FindTmux(context.Background())
	if !choice.Found() {
		return
	}
	for _, host := range hosts {
		if t, ok := host.(*Tmux); ok {
			t.Terminal = &Tmux{Binary: choice.Path, Socket: filepath.Join(stateDir, "tmux", "term.sock")}
		}
	}
}

func (t *Tmux) sessionInventory(ctx context.Context, managedOnly bool) (session.Inventory, error) {
	inv, err := t.inventory(ctx, managedOnly)
	if t.Terminal == nil {
		return inv, err
	}
	// A Terminal opened by this daemon is managed even when discovery of
	// external sessions is off. Bare shells remain non-assistant inventory rows.
	other, otherErr := t.Terminal.inventory(ctx, false)
	inv.Complete = inv.Complete && other.Complete && otherErr == nil
	for _, row := range other.Sessions {
		if !IsPaneID(row.ID) {
			inv.Complete = false
			continue
		}
		row.ID = session.TerminalPanePrefix + row.ID
		inv.Sessions = append(inv.Sessions, row)
	}
	for _, note := range other.Notes {
		inv.Notes = append(inv.Notes, "Terminal: "+note)
	}
	if err == nil {
		err = otherErr
	}
	return inv, err
}

// Resolve only a strictly namespaced pane to the configured server. Stripping
// the namespace without switching servers would type into an unrelated %0.
func (t *Tmux) sessionTarget(s session.Session) (*Tmux, session.Session) {
	if !strings.HasPrefix(s.ID, session.TerminalPanePrefix) {
		return t, s
	}
	if t.Terminal == nil || !session.IsTerminalPane(s.ID) {
		return nil, s
	}
	s.ID = strings.TrimPrefix(s.ID, session.TerminalPanePrefix)
	return t.Terminal, s
}
