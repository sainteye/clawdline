package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline-go/internal/adapters/terminal"
	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// failingHost is one tmux pane whose every send fails the way it is told to.
type failingHost struct {
	*overlapHost
	fail error
}

func (h *failingHost) Send(context.Context, session.Session, string) error { return h.fail }

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
