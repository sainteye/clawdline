package app

import (
	"context"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// switchedTerminal is a terminal source the person can turn off: on, it
// lists its rows; off, it answers a reading that says it was turned off, as
// the iTerm2 adapter does with `iterm_scan` false.
type switchedTerminal struct {
	ports.TerminalHost
	name string
	off  bool
	rows []session.Session
}

func (s *switchedTerminal) Name() string { return s.name }

func (s *switchedTerminal) Inventory(context.Context) (session.Inventory, error) {
	if s.off {
		return session.Inventory{ObservedAt: time.Now(), Provenance: s.name, Disabled: "setting",
			Notes: []string{s.name + " scanning is turned off"}}, nil
	}
	return session.Inventory{ObservedAt: time.Now(), Provenance: s.name, Complete: true,
		Sessions: append([]session.Session(nil), s.rows...)}, nil
}

// A source turned off is named on the merged reading and makes no existence
// decision: the reading is not complete, the source is not among those that
// answered, and nothing proves one of its sessions gone — while the sources
// that were asked still answer for their own rows (D56).
func TestATurnedOffSourceProvesNothingGone(t *testing.T) {
	tmuxPane := session.Session{ID: "%1", TTY: "ttys001", Backend: session.BackendTmux}
	proc := session.Session{ID: "ttys003", TTY: "ttys003", Backend: session.BackendITerm,
		Assistant: session.AssistantClaude}
	iterm := &switchedTerminal{name: "iterm", off: true}
	in := Inventory{
		Process: stubProcess{inv: session.Inventory{Provenance: "ps", Complete: true,
			Sessions: []session.Session{proc}}},
		Terminals: []ports.TerminalHost{&switchedTerminal{name: "tmux", rows: []session.Session{tmuxPane}}, iterm},
	}
	inv := in.Read(context.Background())
	if inv.Complete {
		t.Fatal("a reading with iTerm2 turned off says it is all there is")
	}
	if inv.DisabledSources["iterm"] != "setting" {
		t.Fatalf("the disabled source is not named: %v", inv.DisabledSources)
	}
	if _, answered := inv.Sources["iterm"]; answered {
		t.Fatalf("a turned-off source is counted as having answered: %v", inv.Sources)
	}
	if proves, why := inv.ProvesAbsence("iterm"); proves || why == "" {
		t.Fatalf("a turned-off iTerm2 proved a tab gone (%v, %q)", proves, why)
	}
	if proves, _ := inv.ProvesAbsence("tmux"); !proves {
		t.Fatal("tmux, which answered, can no longer prove its own pane gone")
	}
	if len(inv.Sessions) != 2 {
		t.Fatalf("rows from the sources that answered: %+v", inv.Sessions)
	}
}

// Turning a source off is not a failed reading: its last rows are not kept
// and aged into `missing` (D55), they are simply not being looked at, and a
// reading long past the retention window still names the source as off
// rather than missing.
func TestATurnedOffSourceNeverAgesIntoMissing(t *testing.T) {
	clock := time.Unix(3_000_000, 0)
	tab := session.Session{ID: "0A1B2C3D-0000-4000-8000-000000000001", TTY: "ttys002",
		Backend: session.BackendITerm, Assistant: session.AssistantClaude}
	tmuxPane := session.Session{ID: "%1", TTY: "ttys001", Backend: session.BackendTmux}
	iterm := &switchedTerminal{name: "iterm", rows: []session.Session{tab}}
	in := Inventory{
		Process:   stubProcess{inv: session.Inventory{Provenance: "ps", Complete: true}},
		Terminals: []ports.TerminalHost{&switchedTerminal{name: "tmux", rows: []session.Session{tmuxPane}}, iterm},
	}
	r := NewInventoryReading(in.Read, time.Second)
	r.now = func() time.Time { return clock }
	if first := r.Fresh(context.Background()); !first.Complete || len(first.Sessions) != 2 {
		t.Fatalf("the first reading: %+v", first)
	}

	iterm.off = true
	for _, step := range []time.Duration{2 * time.Second, 10 * time.Minute} {
		clock = clock.Add(step)
		r.Fresh(context.Background())
		held, _ := r.Held() // what a drawing is shown
		if held.DisabledSources["iterm"] != "setting" {
			t.Fatalf("after %s the reading does not say iTerm2 is off: %+v", step, held)
		}
		if held.Observation.Freshness == session.FreshnessMissing {
			t.Fatalf("after %s a turned-off source made the reading missing", step)
		}
		for _, row := range held.Sessions {
			if row.Backend == session.BackendITerm {
				t.Fatalf("after %s a turned-off source's row is still drawn: %+v", step, row)
			}
			if row.Observation.Freshness != session.FreshnessCurrent {
				t.Fatalf("after %s the tmux row is %s", step, row.Observation.Freshness)
			}
		}
	}
}
