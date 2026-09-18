// Package app composes the ports into the readings and commands the transports
// expose. It depends on the domain and on the port interfaces, never on a
// platform implementation.
package app

import (
	"context"
	"sort"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

type Inventory struct {
	Process   ports.ProcessHost
	Terminals []ports.TerminalHost
	Identity  ports.IdentityHost
	Screen    ports.ScreenHost
}

// Read takes one reading of the machine from every source and merges them.
//
// An assistant running inside a tmux pane is seen twice: once by the process
// table, which knows it is running, and once by tmux, which also knows its
// pane id, title and working directory. The tmux row wins on the merge because
// it carries more, and the two are recognised as one session by their tty.
//
// Completeness is the AND of its sources: one unreadable source makes the whole
// reading non-authoritative, because what it casts doubt on is whether the list
// is complete at all — not one row in it. Each source's own answer is kept
// beside the AND (Sources), because "is this tmux pane gone" is a question
// only tmux can answer, and the AND would let an iTerm2 that cannot be asked
// veto it for ever — or, read the other way, let a reading with iTerm2 rows
// in it stand in for a tmux listing that failed.
func (in Inventory) Read(ctx context.Context) session.Inventory {
	// The identity source is refreshed here rather than by whoever calls this.
	// It used to be the caller's job, and of the six places that read an
	// inventory only two remembered — so the board, the coordinator, the
	// session actions and the usage report were all reading identities from an
	// earlier moment, which shows up as sessions that have no conversation id
	// and therefore no record to read. A precondition that every caller must
	// remember is a precondition that will be forgotten.
	if h, ok := in.Identity.(interface{ Refresh() }); ok {
		h.Refresh()
	}

	merged := session.Inventory{
		ObservedAt: time.Now(),
		Provenance: "merged",
		Complete:   true,
		Sources:    map[string]bool{},
	}

	byTTY := map[string]session.Session{}
	order := []string{}

	add := func(src session.Inventory) {
		if !src.Complete {
			merged.Complete = false
		}
		// Two sources with one name are one source: either failing makes it
		// incomplete.
		if prev, seen := merged.Sources[src.Provenance]; seen {
			merged.Sources[src.Provenance] = prev && src.Complete
		} else {
			merged.Sources[src.Provenance] = src.Complete
		}
		merged.Notes = append(merged.Notes, src.Notes...)
		for _, s := range src.Sessions {
			key := s.TTY
			if key == "" {
				key = s.ID
			}
			existing, seen := byTTY[key]
			if !seen {
				order = append(order, key)
				byTTY[key] = s
				continue
			}
			byTTY[key] = richer(existing, s)
		}
	}

	if in.Process != nil {
		inv, _ := in.Process.Scan(ctx)
		add(inv)
	}
	for _, t := range in.Terminals {
		inv, _ := t.Inventory(ctx)
		add(inv)
	}

	sort.Strings(order)
	for _, key := range order {
		row := in.enrich(ctx, byTTY[key])
		row = in.readScreen(ctx, row)
		row = in.readShells(ctx, row)
		merged.Sessions = append(merged.Sessions, row)
	}
	return merged
}

// enrich asks the assistant's own records about a row the machine reported.
//
// What comes back outranks the process reading, and the row says so: a status
// the assistant wrote about itself is not the same kind of fact as a process
// that merely exists, and a reader has to be able to tell them apart.
//
// An assistant's name comes from the identity source alone. What a terminal
// reported as the row's title — a pane title, a tab name — is dropped here,
// because the Swift app names no session from one: a title is a place a name
// is displayed, anything in the terminal can overwrite it, and a tab renamed
// to `Default` must not rename the row. With no identity there is no name,
// and the console says where the session is instead.
func (in Inventory) enrich(ctx context.Context, s session.Session) session.Session {
	if !s.IsAssistant() {
		return s
	}
	s.Label = ""
	if in.Identity == nil {
		return s
	}
	id, ok := in.Identity.ForSession(ctx, s)
	if !ok {
		return s
	}
	if id.ConversationID != "" {
		s.ConversationID = id.ConversationID
	}
	s.Label = id.Label
	s.Rungs = id.Rungs
	s.CustomTitle = id.CustomTitle
	if s.CWD == "" && id.CWD != "" {
		s.CWD = id.CWD
	}
	if id.Pane != "" {
		s.ID = id.Pane
		s.Backend = session.BackendTmux
	}
	if id.Status != "" {
		s.State = session.StateFromAssistantStatus(id.Status)
		s.Evidence = session.EvidenceRegistry
	}
	return s
}

// readScreen is the fallback for an assistant that keeps no live record of
// itself. It runs only where nothing better answered, so a session that told us
// what it is doing is never overruled by a guess about its screen.
//
// The live line is a different question and is read either way. A session's
// own record can say it is working; only the screen says what it is working
// on, and the Swift app draws that line under every working row whatever told
// it the state. So a registry-backed session is captured too — but only while
// it is working, which is the only time there is a line to find, or waiting,
// which is the only time there is a menu to find.
//
// **A menu beats a spinner, and beats a registry that says busy.** Claude Code
// draws its dialog below whatever came before it without always erasing the
// spinner line, and the registry can be a beat behind a dialog just drawn; of
// the two ways to be wrong for that beat, only "a waiting session shown as
// working" hides the row somebody has to act on (SessionState.read,
// SessionRegistry.merge). A registry that says waiting opens the parsing gate
// for AskUserQuestion's flush-left caret; nothing else here does, because this
// daemon installs no hooks.
func (in Inventory) readScreen(ctx context.Context, s session.Session) session.Session {
	if in.Screen == nil || !s.IsAssistant() {
		return s
	}
	registry := s.Evidence == session.EvidenceRegistry
	if registry && s.State != session.StateWorking && s.State != session.StateWaiting {
		return s
	}
	screen, ok := in.Screen.Capture(ctx, s)
	if !ok {
		return s
	}
	gate := registry && s.State == session.StateWaiting
	if menu, found := session.ReadMenu(screen, s.Assistant, gate); found {
		if s.State != session.StateWaiting {
			s.State = session.StateWaiting
			s.Evidence = session.EvidenceScreen
		}
		menu = in.refillMenu(ctx, s, menu)
		s.Menu = &menu
		// The Swift page's transcript revision watches `line`, and a waiting
		// row never draws it, so it carries the menu's revision instead.
		s.Line = session.MenuRevision(menu)
		return s
	}
	if !registry {
		if state, read := session.ReadState(screen, s.Assistant); read {
			s.State = state
			s.Evidence = session.EvidenceScreen
		}
	}
	if s.State == session.StateWorking {
		s.Line = session.WorkingLine(screen, s.Assistant, 25)
	}
	return s
}

// refillMenu gives a menu the words the screen had no room for, from the
// questions the session's own transcript says are open — and only from the one
// the screen proves it is showing (session.RefillMenu).
func (in Inventory) refillMenu(ctx context.Context, s session.Session, menu session.Menu) session.Menu {
	h, ok := in.Identity.(interface {
		OpenQuestions(context.Context, session.Session) ([]session.AskedQuestion, bool)
	})
	if !ok || len(menu.Options) == 0 {
		return menu
	}
	asked, ok := h.OpenQuestions(ctx, s)
	if !ok {
		return menu
	}
	return session.RefillMenu(menu, asked)
}

// readShells asks what the session left running in the background, when the
// identity source can say. It is asked of every assistant row whatever its
// state: an idle row with a build still going is the one this exists for.
func (in Inventory) readShells(ctx context.Context, s session.Session) session.Session {
	h, ok := in.Identity.(interface {
		Shells(context.Context, session.Session) []session.Shell
	})
	if !ok || !s.IsAssistant() {
		return s
	}
	s.Shells = h.Shells(ctx, s)
	return s
}

// richer keeps the row that carries more, field by field, rather than letting
// whichever source answered last overwrite what another source proved.
func richer(a, b session.Session) session.Session {
	out := a
	if out.CWD == "" {
		out.CWD = b.CWD
	}
	if out.Label == "" {
		out.Label = b.Label
	}
	if out.PID == 0 {
		out.PID = b.PID
	}
	if out.Assistant == "" {
		out.Assistant = b.Assistant
	}
	// A pane id survives a restart in a way a tty does not, so it is the better
	// identity when both are present.
	if b.Backend == session.BackendTmux {
		out.ID = b.ID
		out.Backend = b.Backend
	}
	// So is an iTerm2 session id, and it is the one the Swift app lists, the
	// one a start answers with, and the one the iTerm2 adapter finds a tab by.
	// Keeping the tty here listed every iTerm2 tab under a name no action
	// could reach. A tmux pane already on the row keeps its id: under
	// `tmux -CC` the iTerm2 mirror has no tty and never reaches this merge.
	if b.Backend == session.BackendITerm && b.ID != "" && b.ID != b.TTY && out.Backend != session.BackendTmux {
		out.ID = b.ID
		out.Backend = b.Backend
	}
	return out
}
