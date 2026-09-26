package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// failingHost is one tmux pane whose every send fails the way it is told to.
type failingHost struct {
	*overlapHost
	fail error
}

func (h *failingHost) Send(context.Context, session.Session, string) error { return h.fail }

type stateHost struct {
	*overlapHost
	state session.State
}

func (h *stateHost) Inventory(context.Context) (session.Inventory, error) {
	return session.Inventory{Complete: true, Provenance: "tmux", Sessions: []session.Session{{
		ID: "%1", TTY: "tty1", Backend: session.BackendTmux, Assistant: session.AssistantClaude, State: h.state,
	}}}, nil
}

// A caller that types — the broker's briefing — decides whether to type the
// same line again on one question: did anything reach the terminal? Send knows
// which of its refusals came before the first byte, and says so as
// terminal.Unsent; a failure inside the terminal's own write keeps the
// terminal's typed error, so a write that failed part way is never mistaken
// for one that sent nothing (the review of e54e338, F2).
func TestSendSaysWhichOfItsRefusalsSentNothing(t *testing.T) {
	ctx := context.Background()
	send := func(fail error, id, text string) error {
		host := &failingHost{overlapHost: newOverlapHost("%1"), fail: fail}
		a := Actions{
			Inventory: Inventory{Terminals: []ports.TerminalHost{host}},
			Terminals: []ports.TerminalHost{host},
		}
		_, err := a.Send(ctx, id, text)
		return err
	}
	var unsent terminal.Unsent
	for name, err := range map[string]error{
		"not found":     send(nil, "%9", "line"),
		"empty":         send(nil, "%1", ""),
		"terminal said": send(terminal.Unsent{Why: "That session is gone"}, "%1", "line"),
	} {
		if !errors.As(err, &unsent) {
			t.Errorf("%s: %v does not say that nothing was sent", name, err)
		}
	}
	err := send(terminal.Failure{Message: "iTerm2 did not answer in time."}, "%1", "line")
	if errors.As(err, &unsent) {
		t.Fatalf("a write that failed inside the terminal was called unsent: %v", err)
	}
	var failure terminal.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("the terminal's own failure was dropped from %v", err)
	}
}

func TestAssignmentCourtesySendRequiresAFreshIdleReading(t *testing.T) {
	for _, state := range []session.State{session.StateWorking, session.StateWaiting, session.StateUnknown} {
		t.Run(string(state), func(t *testing.T) {
			host := &stateHost{overlapHost: newOverlapHost("%1"), state: state}
			a := Actions{Inventory: Inventory{Terminals: []ports.TerminalHost{host}},
				Terminals: []ports.TerminalHost{host}}
			_, err := a.SendIfIdle(context.Background(), "%1", "next Board item")
			var unsent terminal.Unsent
			if !errors.As(err, &unsent) {
				t.Fatalf("%s: %v does not say that nothing was sent", state, err)
			}
			if host.total.Load() != 0 {
				t.Fatalf("%s: typed %d times", state, host.total.Load())
			}
		})
	}

	host := &stateHost{overlapHost: newOverlapHost("%1"), state: session.StateIdle}
	a := Actions{Inventory: Inventory{Terminals: []ports.TerminalHost{host}},
		Terminals: []ports.TerminalHost{host}}
	if _, err := a.SendIfIdle(context.Background(), "%1", "next Board item"); err != nil {
		t.Fatal(err)
	}
	if host.total.Load() != 1 {
		t.Fatalf("idle: typed %d times", host.total.Load())
	}
}

// A line that was typed and never submitted is its own refusal. It is sitting
// in the session's input line, so a person told only "send failed" types it
// again and the program gets it twice in one line; told send_unsubmitted, the
// person presses Enter on the machine or clears it there.
func TestALineTypedAndNotSubmittedIsRefusedAsSuch(t *testing.T) {
	host := &failingHost{overlapHost: newOverlapHost("%1"),
		fail: terminal.Unsubmitted{Why: "the text was typed, but Enter was not pressed"}}
	a := Actions{Inventory: Inventory{Terminals: []ports.TerminalHost{host}}, Terminals: []ports.TerminalHost{host}}
	_, err := a.Send(context.Background(), "%1", "! git status")
	var ref Refusal
	if !errors.As(err, &ref) || ref.Code != "send_unsubmitted" {
		t.Fatalf("err %v, want the send_unsubmitted refusal", err)
	}
	var unsubmitted terminal.Unsubmitted
	if !errors.As(err, &unsubmitted) {
		t.Fatalf("the terminal's own Unsubmitted was dropped from %v", err)
	}
}
