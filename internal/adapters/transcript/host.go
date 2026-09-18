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
	titles *Titles
	shells *Shells
}

func NewHost() *Host {
	home, _ := os.UserHomeDir()
	return &Host{Home: home, titles: NewTitles(), shells: NewShells()}
}

// Titles is the conversation-title cache, for the capacity register's
// `cache.transcript_titles` row and its override.
func (h *Host) Titles() *Titles { return h.titles }

// Refresh reads both indexes once per inventory rather than once per row.
func (h *Host) Refresh() {
	h.claude = ClaudeRegistryByPID(h.Home)
	h.codex = CodexNames(h.Home)
}

// ForSession names a session the way the Swift app's session list does — see
// session.PreferredLabel for the order, and for why no terminal title is in it.
//
// The two rungs above the conversation's own title are records the Swift app
// keeps in its own store: a name somebody typed in Clawdline, and the title of
// the task Clawdline opened the tab for. They are not read here; the session
// list fills them from that store (internal/adapters/swiftstore) and chooses
// again from Rungs. A weak `aiTitle` is still never set aside, because telling
// weak from strong needs the conversation's first message, which is not read.
func (h *Host) ForSession(ctx context.Context, s session.Session) (ports.Identity, bool) {
	switch s.Assistant {
	case session.AssistantClaude:
		r, ok := h.claude[s.PID]
		if !ok {
			return ports.Identity{}, false
		}
		title, custom := "", ""
		if r.CWD != "" && r.SessionID != "" {
			title, custom = h.titles.Read(ClaudePath(h.Home, r.CWD, r.SessionID))
		}
		rungs := session.LabelRungs{
			Conversation: session.DisplayedConversationTitle(title, false, ""),
			Handle:       r.Name,
		}
		return ports.Identity{
			ConversationID: r.SessionID,
			Pane:           r.Pane(),
			CWD:            r.CWD,
			Label:          session.PreferredLabel(rungs),
			Rungs:          rungs,
			CustomTitle:    custom,
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
		rungs := session.LabelRungs{Thread: h.codex[s.ConversationID]}
		return ports.Identity{
			ConversationID: s.ConversationID,
			Label:          session.PreferredLabel(rungs),
			Rungs:          rungs,
		}, true
	}
	return ports.Identity{}, false
}

// Shells is what a Claude session left running in the background. Codex keeps
// no such files, so it is not asked.
func (h *Host) Shells(ctx context.Context, s session.Session) []session.Shell {
	if s.Assistant != session.AssistantClaude || s.ConversationID == "" {
		return nil
	}
	cwd := s.CWD
	if r, ok := h.claude[s.PID]; ok && r.CWD != "" {
		cwd = r.CWD
	}
	if cwd == "" {
		return nil
	}
	return h.shells.Running(ClaudePath(h.Home, cwd, s.ConversationID))
}
