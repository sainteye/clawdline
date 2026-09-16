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
// is complete at all — not one row in it.
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
	}

	byTTY := map[string]session.Session{}
	order := []string{}

	add := func(src session.Inventory) {
		if !src.Complete {
			merged.Complete = false
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
		merged.Sessions = append(merged.Sessions, row)
	}
	return merged
}

// enrich asks the assistant's own records about a row the machine reported.
//
// What comes back outranks the process reading, and the row says so: a status
// the assistant wrote about itself is not the same kind of fact as a process
// that merely exists, and a reader has to be able to tell them apart.
func (in Inventory) enrich(ctx context.Context, s session.Session) session.Session {
	if in.Identity == nil || !s.IsAssistant() {
		return s
	}
	id, ok := in.Identity.ForSession(ctx, s)
	if !ok {
		return s
	}
	if id.ConversationID != "" {
		s.ConversationID = id.ConversationID
	}
	if id.Label != "" {
		s.Label = id.Label
	}
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
func (in Inventory) readScreen(ctx context.Context, s session.Session) session.Session {
	if in.Screen == nil || !s.IsAssistant() || s.Evidence == session.EvidenceRegistry {
		return s
	}
	screen, ok := in.Screen.Capture(ctx, s)
	if !ok {
		return s
	}
	if state, read := session.ReadState(screen, s.Assistant); read {
		s.State = state
		s.Evidence = session.EvidenceScreen
	}
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
	return out
}
