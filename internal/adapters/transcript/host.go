package transcript

import (
	"context"
	"os"
	"sync"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Host answers the identity port from the assistants' own records on disk.
//
// One Host serves every inventory reading the daemon takes, and several run
// at once (transport/http server.go). So the four indexes below are built
// whole by Refresh and swapped in under mu, and never written in place after
// that: a reader holds a map Refresh no longer touches. Before this, one
// reading's Refresh wrote a map another reading was ranging over, which Go
// answers with `concurrent map read and map write` and the daemon dies.
// Readers take whichever indexes are newest; what belongs to one reading
// alone travels in its context instead (ObserveRows).
type Host struct {
	Home string

	mu        sync.RWMutex
	claude    map[int]ClaudeRegistry
	codex     map[string]string
	codexLive map[string]codexLiveIdentity
	// codexRollouts remembers paths only while their writer locks are live.
	// An ended thread leaves on the next refresh, so this cannot accumulate
	// historical conversations behind the session list.
	codexRollouts map[string]string
	titles        *Titles
	shells        *Shells
	movements     *Movements
}

func NewHost() *Host {
	home, _ := os.UserHomeDir()
	return &Host{Home: home, titles: NewTitles(), shells: NewShells(), movements: NewMovements()}
}

// Titles is the conversation-title cache, for the capacity register's
// `cache.transcript_titles` row and its override.
func (h *Host) Titles() *Titles { return h.titles }

// Movements is the cache behind "when did this session last move", for the
// register's `cache.session_activity` row and its override.
func (h *Host) Movements() *Movements { return h.movements }

// Refresh reads both indexes once per inventory rather than once per row.
// Everything is read first and published in one swap, so a reading in
// progress never sees half of a refresh.
func (h *Host) Refresh() {
	claude := ClaudeRegistryByPID(h.Home)
	codex := CodexNames(h.Home)
	h.mu.RLock()
	rollouts := h.codexRollouts
	h.mu.RUnlock()
	codex, live, rollouts := refreshCodexLive(h.Home, codex, rollouts)
	h.mu.Lock()
	h.claude, h.codex, h.codexLive, h.codexRollouts = claude, codex, live, rollouts
	h.mu.Unlock()
}

// claudeFor is the registry entry for one pid from the newest index.
func (h *Host) claudeFor(pid int) (ClaudeRegistry, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	r, ok := h.claude[pid]
	return r, ok
}

// codexIndexes are the newest name and live-thread indexes. Both are
// read-only to the caller: Refresh replaces them rather than writing them.
func (h *Host) codexIndexes() (map[string]string, map[string]codexLiveIdentity) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.codex, h.codexLive
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
		r, ok := h.claudeFor(s.PID)
		if !ok {
			// Claude Code writes this file itself, so its absence is the
			// session not having written it — not a reading that failed.
			return ports.Identity{Binding: session.BindingNoRecord}, false
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
			Binding:        session.BindingRegistry,
		}, true

	case session.AssistantCodex:
		// Direct Codex sessions are named by the scan: either the command line
		// of a resumed session or the rollout the foreground process holds
		// open. A managed app-server holds that rollout instead of the TUI, so
		// an otherwise-unbound iTerm row gets a second, provider-owned path:
		// live writer lock, rollout head, cwd and exact terminal title must all
		// agree, the title carrying the thread's name when Codex has one. An
		// unnamed thread's bare `<dir> (codex)` title binds only while it is the
		// sole unnamed live root in that cwd and the sole row showing that title. Neither path guesses from cwd
		// alone; two Codex sessions in one checkout is the ordinary case here.
		names, lives := h.codexIndexes()
		binding := s.Binding
		if s.ConversationID == "" {
			live, ok := codexLiveFor(ctx, lives, s)
			if !ok {
				return ports.Identity{}, false
			}
			s.ConversationID = live.ID
			binding = session.BindingLiveTitle
		}
		rungs := session.LabelRungs{Thread: names[s.ConversationID]}
		return ports.Identity{
			ConversationID: s.ConversationID,
			Label:          session.PreferredLabel(rungs),
			Rungs:          rungs,
			Binding:        binding,
		}, true
	}
	return ports.Identity{}, false
}

// Shells is what a Claude session left running in the background. Codex keeps
// no such files, so it is not asked.
func (h *Host) Shells(ctx context.Context, s session.Session) []session.Shell {
	path, ok := h.claudeTranscript(s)
	if !ok {
		return nil
	}
	return h.shells.Running(path)
}

// ShellFiles is the reader behind Shells and ShellOutput, so a test can point
// it at a folder of its own.
func (h *Host) ShellFiles() *Shells { return h.shells }

// ShellOutput is the tail of one command this session started in the
// background (Shells.Output). Codex keeps no such files, so a Codex session
// has none to read.
func (h *Host) ShellOutput(ctx context.Context, s session.Session, id string, window int64) (ShellOutput, bool) {
	path, ok := h.claudeTranscript(s)
	if !ok {
		return ShellOutput{}, false
	}
	return h.shells.Output(path, id, window)
}

// claudeTranscript is where a Claude session's transcript is, from the
// registry's working directory when it has one and the scan's otherwise.
func (h *Host) claudeTranscript(s session.Session) (string, bool) {
	if s.Assistant != session.AssistantClaude || s.ConversationID == "" {
		return "", false
	}
	cwd := s.CWD
	if r, ok := h.claudeFor(s.PID); ok && r.CWD != "" {
		cwd = r.CWD
	}
	if cwd == "" {
		return "", false
	}
	return ClaudePath(h.Home, cwd, s.ConversationID), true
}
