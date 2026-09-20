package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// blindHost is a terminal source that will never read one of its windows.
//
// It is the failure this whole file is about, injected rather than waited for:
// on the machine it was measured on, one iTerm2 window answered null to
// `tabs()` in twelve readings out of twelve over two hours, and there is no way
// to make that happen on demand. So a source is written that always does it,
// and the test asks what the rest of the machine can still prove.
type blindHost struct {
	name string
	rows []session.Session
	// sealed is whether another source accounted for the window. The adapter
	// that seals a gap for real does it from the process table
	// (iterm_seal_darwin.go); here it is the input under test.
	sealed bool
}

func (h *blindHost) Name() string { return h.name }
func (h *blindHost) Inventory(context.Context) (session.Inventory, error) {
	gap := session.Gap{Source: h.name, Scope: "window", ID: "27898",
		Detail: "iTerm2 window 27898 would not list its tabs (tabs() answered null)"}
	if h.sealed {
		gap.Sealed, gap.SealedBy = true, "the process table attributes 11 pseudo-terminals to iTerm2 and this listing read every one"
	}
	return session.Inventory{
		Provenance: h.name,
		Complete:   gap.Sealed,
		Sessions:   h.rows,
		Gaps:       []session.Gap{gap},
	}, nil
}
func (h *blindHost) Open(context.Context, ports.OpenRequest) (session.Session, error) {
	return session.Session{}, errors.New("not in this test")
}
func (h *blindHost) Close(context.Context, session.Session) error { return nil }
func (h *blindHost) Send(context.Context, session.Session, string) error {
	return nil
}
func (h *blindHost) Interrupt(context.Context, session.Session) error    { return nil }
func (h *blindHost) Reveal(context.Context, session.Session, bool) error { return nil }
func (h *blindHost) Screen(context.Context, session.Session, int) (string, bool) {
	return "", false
}

// wholeHost is a source that read everything it owns.
type wholeHost struct {
	name string
	rows []session.Session
}

func (h *wholeHost) Name() string { return h.name }
func (h *wholeHost) Inventory(context.Context) (session.Inventory, error) {
	return session.Inventory{Provenance: h.name, Complete: true, Sessions: h.rows}, nil
}
func (h *wholeHost) Open(context.Context, ports.OpenRequest) (session.Session, error) {
	return session.Session{}, errors.New("not in this test")
}
func (h *wholeHost) Close(context.Context, session.Session) error { return nil }
func (h *wholeHost) Send(context.Context, session.Session, string) error {
	return nil
}
func (h *wholeHost) Interrupt(context.Context, session.Session) error    { return nil }
func (h *wholeHost) Reveal(context.Context, session.Session, bool) error { return nil }
func (h *wholeHost) Screen(context.Context, session.Session, int) (string, bool) {
	return "", false
}

// noProcesses is a process table with nothing in it, read completely.
type noProcesses struct{}

func (noProcesses) Scan(context.Context) (session.Inventory, error) {
	return session.Inventory{Provenance: "ps", Complete: true}, nil
}

func blindMachine(sealed bool) Inventory {
	return Inventory{
		Process: noProcesses{},
		Terminals: []ports.TerminalHost{
			&blindHost{name: "iterm", sealed: sealed, rows: []session.Session{
				{ID: "7A5C0000-0000-4000-8000-000000000015", TTY: "ttys015", Backend: session.BackendITerm,
					Assistant: session.AssistantClaude, PID: 21},
			}},
			&wholeHost{name: "tmux", rows: []session.Session{
				{ID: "%8", TTY: "ttys008", Backend: session.BackendTmux,
					Assistant: session.AssistantClaude, PID: 22},
			}},
		},
	}
}

// One source that can never read one of its windows must not take the other
// sources' answers with it.
//
// This is the whole of D05 ③ at the one call site that decides whether a
// session somebody closed is gone or merely unseen. Before this, `Find` asked
// the merged reading, whose Complete is the AND over every source — so a tmux
// pane that tmux had listed completely came back as `session_unknown`, and on
// the machine this was measured on that answer could never change, because the
// window in the way is the person's own and cannot be closed.
func TestABlindSourceOnlyCostsItsOwnSessionsTheirAbsence(t *testing.T) {
	a := Actions{Inventory: blindMachine(false)}
	ctx := context.Background()

	// tmux read everything it owns, so a pane that is not on the list is gone.
	_, err := a.Find(ctx, "%9")
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "session_not_found" {
		t.Errorf("a tmux pane tmux did not list: %v, want session_not_found", err)
	}

	// The blind source's own ids are the ones that stay unprovable, and the
	// refusal names the window that is in the way rather than only the fact
	// that something is.
	_, err = a.Find(ctx, "7A5C0000-0000-4000-8000-000000000099")
	if !errors.As(err, &ref) || ref.Code != "session_unknown" {
		t.Fatalf("an iTerm2 session behind an unread window: %v, want session_unknown", err)
	}
	if !strings.Contains(ref.Detail, "27898") {
		t.Errorf("the refusal does not say which window is in the way: %q", ref.Detail)
	}

	// A row that is on the list is found whatever else was not read.
	if _, err := a.Find(ctx, "%8"); err != nil {
		t.Errorf("a pane that is on the list: %v", err)
	}
}

// And once another source has accounted for the window, the blind source
// answers for itself again — with the window still named.
func TestASealedWindowGivesTheSourceItsAnswersBack(t *testing.T) {
	inv := blindMachine(true).Read(context.Background())
	if !inv.Complete {
		t.Errorf("a reading whose every gap is sealed is still incomplete: %v", inv.Notes)
	}
	if len(inv.Gaps) != 1 || !inv.Gaps[0].Sealed || inv.Gaps[0].ID != "27898" {
		t.Fatalf("the sealed window is not carried: %+v", inv.Gaps)
	}
	if proves, why := inv.ProvesAbsence("iterm"); !proves {
		t.Errorf("iterm still does not answer for itself: %s", why)
	}
	a := Actions{Inventory: blindMachine(true)}
	_, err := a.Find(context.Background(), "7A5C0000-0000-4000-8000-000000000099")
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "session_not_found" {
		t.Errorf("an iTerm2 session behind a sealed window: %v, want session_not_found", err)
	}
}

// An id in no source's shape is nobody's question, and falls back to the whole
// reading — which is where every id was before the shapes were read.
func TestAnIDNoSourceIssuesFallsBackToTheWholeReading(t *testing.T) {
	a := Actions{Inventory: blindMachine(false)}
	_, err := a.Find(context.Background(), "something-nobody-issued")
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "session_unknown" {
		t.Errorf("an id of no known shape: %v, want session_unknown", err)
	}
	if !strings.Contains(ref.Detail, "this machine") {
		t.Errorf("the refusal does not say it is the whole machine that cannot answer: %q", ref.Detail)
	}
}
