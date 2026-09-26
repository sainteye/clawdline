package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/task"
)

// What a close that did not close says (actions.go, closeRefusal).
//
// One `close_failed` for every rung was the whole of it before, and the four
// things a person can do about a close that did not happen are four different
// things. Each row here is a refusal given before the terminal was taken away.
func TestEachRungOfACloseSaysWhichOneItWas(t *testing.T) {
	for _, c := range []struct {
		err  error
		code string
	}{
		{terminal.Unreadable{Why: "the tty could not be read"}, "close_unreadable"},
		{terminal.Occupied{Why: "vim is in front of it"}, "close_occupied"},
		{terminal.QuitRefused{Why: "it would not take /exit"}, "close_quit_refused"},
		{terminal.StillRunning{Why: "it did not leave"}, "close_assistant_running"},
		{terminal.Unconfirmed{Why: "no answer"}, "close_unconfirmed"},
		{terminal.Unconfirmed{Why: "a sheet is up", Attention: true}, "close_needs_a_person"},
		{terminal.Unsent{Why: "That session is gone"}, "close_nothing_there"},
		{terminal.Failure{Attention: true, Message: "iTerm2 did not answer in time."}, "close_needs_a_person"},
		{terminal.Failure{Message: "iTerm2 refused."}, "close_failed"},
		{errors.New("something nobody typed"), "close_failed"},
	} {
		ref, ok := closeRefusal(c.err).(Refusal)
		if !ok || ref.Code != c.code {
			t.Errorf("%T %v: %v, want %s", c.err, c.err, closeRefusal(c.err), c.code)
		}
		if ref.Cause == nil {
			t.Errorf("%s dropped the terminal's own error", c.code)
		}
	}
}

// closeHost records the order of everything a close does to a terminal.
type closeHost struct {
	calls []string
	err   error
	// conversation is the one the session in it is running, if any.
	conversation string
}

func (h *closeHost) Name() string { return "tmux" }
func (h *closeHost) Inventory(context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Provenance: "tmux", Sessions: []session.Session{
		{ID: "%1", TTY: "ttys1", Backend: session.BackendTmux, Assistant: session.AssistantClaude, PID: 400,
			CWD: "/work", ConversationID: h.conversation},
	}}, nil
}
func (h *closeHost) Open(context.Context, ports.OpenRequest) (session.Session, error) {
	return session.Session{}, nil
}
func (h *closeHost) Send(context.Context, session.Session, string) error { return nil }
func (h *closeHost) Interrupt(context.Context, session.Session) error    { return nil }
func (h *closeHost) Close(context.Context, session.Session) error {
	h.calls = append(h.calls, "close")
	return h.err
}
func (h *closeHost) Reveal(context.Context, session.Session, bool) error         { return nil }
func (h *closeHost) Screen(context.Context, session.Session, int) (string, bool) { return "", false }

func closeActions(h *closeHost) Actions {
	return Actions{
		Inventory: Inventory{Terminals: []ports.TerminalHost{h}},
		Terminals: []ports.TerminalHost{h},
		Owed:      func(context.Context) ([]task.Obligation, error) { return nil, nil },
	}
}

// A close types into the session now — the assistant's own quit word — so it
// takes the terminal's lane like every other write. Without it a close and a
// message would be two writers on one terminal.
func TestACloseTakesTheTerminalsLane(t *testing.T) {
	h := &closeHost{}
	a := closeActions(h)
	release, err := a.lanes().Acquire(context.Background(),
		lane.TerminalKey(string(session.BackendTmux), "%1"))
	if err != nil {
		t.Fatalf("holding the lane: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Close(ctx, "%1", false); err == nil {
		t.Fatal("a close walked into a terminal somebody else was writing to")
	} else if ref, ok := err.(Refusal); !ok || ref.Code != "busy" {
		t.Fatalf("a full lane: %v", err)
	}
	if len(h.calls) != 0 {
		t.Fatalf("the terminal was touched anyway: %v", h.calls)
	}
	release()
	if _, err := a.Close(context.Background(), "%1", false); err != nil {
		t.Fatalf("with the lane free: %v", err)
	}
	if len(h.calls) != 1 {
		t.Fatalf("calls: %v", h.calls)
	}
}

// A close through Clawdline is the person saying they are done with that
// conversation, so the record of this boot marks it closed and no later
// restore offers it, however close to a reboot it happened.
func TestACloseMarksTheConversationClosedInTheRestoreRecord(t *testing.T) {
	st := restoreStore(t)
	clock := time.Unix(80_000, 0)
	r := restoreUnder(st, "boot-now", &clock)
	h := &closeHost{conversation: "conv-1"}
	a := closeActions(h)
	a.Restore = r
	ctx := context.Background()
	r.Observe(ctx, complete(session.Session{ID: "%1", Backend: session.BackendTmux,
		Assistant: session.AssistantClaude, CWD: "/work", ConversationID: "conv-1"}))
	clock = clock.Add(time.Second)
	if _, err := a.Close(ctx, "%1", false); err != nil {
		t.Fatal(err)
	}
	if row := recorded(t, st, "boot-now")["conv-1"]; !row.ClosedAt.Equal(clock) {
		t.Fatalf("after the close: %+v", row)
	}
}

// looseScan is a process table with one assistant on a tty no terminal lists:
// a claude in a tmux server started on a socket of its own.
type looseScan struct{}

func (looseScan) Scan(context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Provenance: "ps", Sessions: []session.Session{
		{ID: "ttys011", TTY: "ttys011", Backend: session.BackendITerm, Assistant: session.AssistantClaude, PID: 77548},
	}}, nil
}

// itermNamed is closeHost answering as iTerm2, which has no session by a tty's
// name and says so the way the real one does.
type itermNamed struct{ closeHost }

func (h *itermNamed) Name() string { return "iterm" }
func (h *itermNamed) Inventory(context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Provenance: "iterm"}, nil
}

type processCloser struct{ closed []string }

func (p *processCloser) CloseProcess(_ context.Context, s session.Session) error {
	p.closed = append(p.closed, s.ID+" pid "+fmt.Sprint(s.PID))
	return nil
}

// A row only the process table saw is closed by asking its process to leave.
// Asked of iTerm2 instead, it answered close_nothing_there about an assistant
// that was still running, every time, and the row stayed on the list.
func TestASessionOnlyTheProcessTableSawIsClosedThroughItsProcess(t *testing.T) {
	iterm := &itermNamed{closeHost: closeHost{err: terminal.Unsent{Why: "That session is gone"}}}
	procs := &processCloser{}
	a := Actions{
		Inventory: Inventory{Process: looseScan{}, Terminals: []ports.TerminalHost{iterm}},
		Terminals: []ports.TerminalHost{iterm},
		Owed:      func(context.Context) ([]task.Obligation, error) { return nil, nil },
		Processes: procs,
	}
	if _, err := a.Close(context.Background(), "ttys011", false); err != nil {
		t.Fatalf("close = %v", err)
	}
	if len(iterm.calls) != 0 {
		t.Fatalf("iTerm2 was asked to close a tty it never listed: %v", iterm.calls)
	}
	if len(procs.closed) != 1 || procs.closed[0] != "ttys011 pid 77548" {
		t.Fatalf("process closes = %v, want the one row's process", procs.closed)
	}

	// With nothing that can end a process, it is refused by name rather than
	// reported as already gone.
	a.Processes = nil
	_, err := a.Close(context.Background(), "ttys011", false)
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "backend_unsupported" {
		t.Fatalf("close with no process closer = %v, want backend_unsupported", err)
	}
}
