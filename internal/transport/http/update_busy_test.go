package http

import (
	"testing"

	"github.com/sainteye/clawdline/internal/domain/session"
)

func TestAutoApplyWaitsOnlyForSessionsARestartCouldDisturb(t *testing.T) {
	agent := func(b session.Backend, st session.State) session.Session {
		return session.Session{Assistant: "claude", Backend: b, State: st}
	}
	merged := func(tmux, iterm bool, ss ...session.Session) session.Inventory {
		return session.Inventory{Sessions: ss, Complete: tmux && iterm,
			Sources: map[string]bool{"ps": true, "tmux": tmux, "iterm": iterm}}
	}
	for _, c := range []struct {
		name string
		inv  session.Inventory
		busy bool
	}{
		{"idle everywhere", merged(true, true, agent(session.BackendTmux, session.StateIdle)), false},
		{"working in tmux", merged(true, true, agent(session.BackendTmux, session.StateWorking)), true},
		{"working in iTerm2", merged(true, true, agent(session.BackendITerm, session.StateWorking)), true},
		{"unknown in tmux", merged(true, true, agent(session.BackendTmux, session.StateUnknown)), true},
		{"unknown in a pty it opened", merged(true, true, agent(session.BackendOwned, session.StateUnknown)), true},
		{"iTerm2 does not answer", merged(true, false, agent(session.BackendITerm, session.StateUnknown),
			agent(session.BackendTmux, session.StateIdle)), false},
		{"tmux reading incomplete", merged(false, true), true},
		{"unmerged incomplete reading", session.Inventory{Complete: false}, true},
		{"unmerged complete reading", session.Inventory{Complete: true}, false},
		{"not an assistant", merged(true, true, session.Session{Backend: session.BackendTmux, State: session.StateUnknown}), false},
	} {
		if got := inventoryBusy(c.inv); got != c.busy {
			t.Errorf("%s: busy = %v, want %v", c.name, got, c.busy)
		}
	}
}
