package app

import (
	"context"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// stallTerminal is a terminal source whose listing answers, fails at once, or
// blocks until its reading's context ends (or the test lets it go) — the
// three things iTerm2's list Apple Event was measured doing on 2026-10-01.
type stallTerminal struct {
	ports.TerminalHost
	name string

	mu      sync.Mutex
	mode    string // "answer", "fail" or "block"
	rows    []session.Session
	release chan struct{}
	blocked chan struct{}
}

func (s *stallTerminal) Name() string { return s.name }

func (s *stallTerminal) set(mode string, rows ...session.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
	if rows != nil {
		s.rows = rows
	}
}

func (s *stallTerminal) Inventory(ctx context.Context) (session.Inventory, error) {
	s.mu.Lock()
	mode, rows, release, blocked := s.mode, append([]session.Session(nil), s.rows...), s.release, s.blocked
	s.mu.Unlock()
	inv := session.Inventory{ObservedAt: time.Now(), Provenance: s.name, Complete: true}
	switch mode {
	case "fail":
		inv.Complete = false
		inv.Notes = []string{s.name + " apple event failed: signal: killed"}
		return inv, nil
	case "block":
		if blocked != nil {
			select {
			case blocked <- struct{}{}:
			default:
			}
		}
		select {
		case <-ctx.Done():
		case <-release:
		}
		inv.Complete = false
		inv.Notes = []string{s.name + " apple event failed: signal: killed"}
		return inv, nil
	}
	inv.Sessions = rows
	return inv, nil
}

func sourcesOf(inv session.Inventory) map[string]bool { return cloneSources(inv.Sources) }

func incompleteOf(inv session.Inventory) []string {
	var out []string
	for name, complete := range inv.Sources {
		if !complete {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func freshnessOf(inv session.Inventory) map[string]session.Freshness {
	out := map[string]session.Freshness{}
	for _, row := range inv.Sessions {
		out[row.ID] = row.Observation.Freshness
	}
	return out
}

// The episode of 2026-10-01 20:10-20:25: iTerm2's Apple Events stopped
// answering, and every Cloud publish for fifteen minutes read
// `incomplete_sources=[iterm ps tmux]` although the process table and tmux
// answered every scan. While iTerm2 is the only source that is stalled, the
// reading a drawing gets must say so of iTerm2 alone: tmux and the process
// table complete and their rows current, iTerm2's last rows unverified, and
// nothing a stalled or newer answer could not vouch for claimed as complete.
func TestAStalledITermLeavesTmuxAndProcessRowsVerified(t *testing.T) {
	const ttl = 40 * time.Millisecond
	tmuxPane := session.Session{ID: "%1", TTY: "ttys001", Backend: session.BackendTmux}
	itermTab := session.Session{ID: "0A1B2C3D-0000-4000-8000-000000000001", TTY: "ttys002",
		Backend: session.BackendITerm}
	// A session in a tab only the process table saw, listed under its tty.
	proc := session.Session{ID: "ttys003", TTY: "ttys003", Backend: session.BackendITerm,
		Assistant: session.AssistantClaude}

	tmux := &stallTerminal{name: "tmux", mode: "answer", rows: []session.Session{tmuxPane}}
	iterm := &stallTerminal{name: "iterm", mode: "answer", rows: []session.Session{itermTab},
		release: make(chan struct{}), blocked: make(chan struct{}, 1)}
	letGo := func() {
		iterm.mu.Lock()
		defer iterm.mu.Unlock()
		select {
		case <-iterm.release:
		default:
			close(iterm.release)
		}
	}
	t.Cleanup(letGo)
	in := Inventory{
		Process: stubProcess{inv: session.Inventory{Provenance: "ps", Complete: true,
			Sessions: []session.Session{proc}}},
		Terminals: []ports.TerminalHost{tmux, iterm},
	}
	r := NewInventoryReading(in.Read, ttl)
	ctx := context.Background()

	if first := r.Fresh(ctx); !first.Complete {
		t.Fatalf("the healthy reading: %+v", first)
	}
	// The listing fails once: the reading is honest about iTerm2 alone.
	iterm.set("fail")
	r.Fresh(ctx)
	held, _ := r.Held() // what a drawing is shown, with the last iTerm2 rows retained
	if got := incompleteOf(held); len(got) != 1 || got[0] != "iterm" {
		t.Fatalf("a failed iTerm2 listing made %v incomplete: %+v", got, held.Sources)
	}
	if f := freshnessOf(held); f[proc.ID] != session.FreshnessCurrent || f[tmuxPane.ID] != session.FreshnessCurrent ||
		f[itermTab.ID] != session.FreshnessUnverified {
		t.Fatalf("held freshness after a failed iTerm2 listing: %v", f)
	}

	// Then it stops answering at all, and every scan waits on it.
	iterm.set("block")
	time.Sleep(2 * ttl)
	r.Fast(ctx) // starts the refresh that will stall on iTerm2
	select {
	case <-iterm.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the refresh never reached iTerm2")
	}
	time.Sleep(3 * ttl) // the refresh is now slower than the TTL
	drawn := r.Fast(ctx)
	if got := incompleteOf(drawn); len(got) != 1 || got[0] != "iterm" {
		t.Fatalf("while iTerm2 stalls the drawing reads incomplete_sources=%v (sources %v)", got, sourcesOf(drawn))
	}
	if drawn.Complete {
		t.Fatalf("a reading with a stalled source claimed to be complete: %+v", drawn)
	}
	if f := freshnessOf(drawn); f[proc.ID] != session.FreshnessCurrent || f[tmuxPane.ID] != session.FreshnessCurrent ||
		f[itermTab.ID] != session.FreshnessUnverified {
		t.Fatalf("row freshness while iTerm2 stalls: %v", f)
	}
	// An iTerm2 that cannot be asked still proves nothing gone.
	a := Actions{Reading: r}
	if _, err := a.FindForRead(ctx, "0A1B2C3D-0000-4000-8000-0000000000FF"); !isRefusal(err, "session_unknown") {
		t.Fatalf("an unseen iTerm2 id during the stall: %v", err)
	}

	// The stalled listing ends (its own timeout), and the next one stalls as
	// well — but this time tmux has a pane the held reading never saw. tmux's
	// newer answer cannot be vouched for by rows that lack it, so the drawing
	// must not call tmux complete, and the new pane is unseen, not gone.
	letGo()
	r.Fresh(ctx)
	iterm.mu.Lock()
	iterm.release = make(chan struct{})
	iterm.mu.Unlock()
	iterm.set("block")
	newPane := session.Session{ID: "%2", TTY: "ttys004", Backend: session.BackendTmux}
	tmux.set("answer", tmuxPane, newPane)
	time.Sleep(2 * ttl)
	r.Fast(ctx)
	select {
	case <-iterm.blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("the second refresh never reached iTerm2")
	}
	time.Sleep(3 * ttl)
	drawn = r.Fast(ctx)
	if s := sourcesOf(drawn); s["tmux"] || !s["ps"] || s["iterm"] {
		t.Fatalf("a tmux answer the drawn rows lack was vouched for: %v", s)
	}
	if _, err := a.FindForRead(ctx, newPane.ID); !isRefusal(err, "session_unknown") {
		t.Fatalf("a pane tmux listed after the held reading was called absent: %v", err)
	}
}

func isRefusal(err error, code string) bool {
	var refusal Refusal
	return errors.As(err, &refusal) && refusal.Code == code
}
