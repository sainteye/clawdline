package transcript

import (
	"context"
	"os"

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// Host answers the identity port from the assistants' own records on disk.
type Host struct {
	Home string

	claude map[int]ClaudeRegistry
	codex  map[string]string
}

func NewHost() *Host {
	home, _ := os.UserHomeDir()
	return &Host{Home: home}
}

// Refresh reads both indexes once per inventory rather than once per row.
func (h *Host) Refresh() {
	h.claude = ClaudeRegistryByPID(h.Home)
	h.codex = CodexNames(h.Home)
}

func (h *Host) ForSession(ctx context.Context, s session.Session) (ports.Identity, bool) {
	switch s.Assistant {
	case session.AssistantClaude:
		r, ok := h.claude[s.PID]
		if !ok {
			return ports.Identity{}, false
		}
		return ports.Identity{
			ConversationID: r.SessionID,
			Pane:           r.Pane(),
			CWD:            r.CWD,
			Label:          ClaudeTitle(h.Home, r.CWD, r.SessionID),
			Status:         r.Status,
		}, true

	case session.AssistantCodex:
		// Codex keeps no live registry, so the only proof of identity is the
		// id it was resumed with, which it carries on its own command line.
		// A session started fresh has none, and this says so by answering
		// false rather than guessing from the working directory.
		if s.ConversationID == "" {
			return ports.Identity{}, false
		}
		return ports.Identity{
			ConversationID: s.ConversationID,
			Label:          h.codex[s.ConversationID],
		}, true
	}
	return ports.Identity{}, false
}
