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
		merged.Sessions = append(merged.Sessions, byTTY[key])
	}
	return merged
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
