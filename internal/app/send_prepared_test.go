package app

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// sendingPane is a menuTerminal that also records its sends, in one sequence
// with its keystrokes, so the order of the two can be asked about.
type sendingPane struct{ *menuTerminal }

func (p sendingPane) Send(ctx context.Context, _ session.Session, text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.typed = append(p.typed, "send:"+text)
	return nil
}

func preparedActions(p sendingPane) Actions {
	return Actions{Inventory: Inventory{Terminals: []ports.TerminalHost{p}, Screen: p},
		Terminals: []ports.TerminalHost{p}, Lanes: lane.New(lane.DefaultLimit)}
}

// The keystroke and the look happen before the text, inside the send; a
// prepare that refuses types nothing, and says why in a way errors.Is reads.
func TestSendPreparedRunsItsStepFirstAndStopsOnItsNo(t *testing.T) {
	pane := sendingPane{&menuTerminal{s: paneFor("%9"), screen: "❯ hello"}}
	a := preparedActions(pane)
	looked := ""
	_, err := a.SendPrepared(context.Background(), "%9", "notice",
		func(ctx context.Context, press func([]byte) error, look func() (string, bool)) error {
			looked, _ = look()
			return press([]byte{0x13})
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := pane.keys(); len(got) != 2 || got[0] != "\x13" || got[1] != "send:notice" {
		t.Fatalf("order: %q", got)
	}
	if looked != "❯ hello" {
		t.Fatalf("prepare looked at %q", looked)
	}

	no := errors.New("not clear")
	_, err = a.SendPrepared(context.Background(), "%9", "notice",
		func(context.Context, func([]byte) error, func() (string, bool)) error { return no })
	if !errors.Is(err, no) {
		t.Fatalf("the refusal lost its cause: %v", err)
	}
	if got := pane.keys(); len(got) != 2 {
		t.Fatalf("a refused prepare still typed: %q", got)
	}
}
