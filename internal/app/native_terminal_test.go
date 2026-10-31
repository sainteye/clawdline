package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	domainterminal "github.com/sainteye/clawdline/internal/domain/terminal"
)

type nativeTerminalFixture struct {
	*menuTerminal
	identity   string
	broken     bool
	writeError error
	attempts   int
}

func (h *nativeTerminalFixture) NativeTerminalIdentity(context.Context, session.Session) (string, error) {
	if h.broken {
		return "", errors.New("assistant gone")
	}
	return h.identity, nil
}
func (h *nativeTerminalFixture) Keystroke(ctx context.Context, s session.Session, b []byte) error {
	h.attempts++
	if h.writeError != nil {
		return h.writeError
	}
	return h.menuTerminal.Keystroke(ctx, s, b)
}
func nativeTerminalActions() (*nativeTerminalFixture, Actions) {
	h := &nativeTerminalFixture{menuTerminal: &menuTerminal{s: paneFor("%1"), screen: "❯ native CLI\n"}, identity: "claude:123:birth"}
	a := menuActions(h.menuTerminal)
	a.Terminals = []ports.TerminalHost{h}
	a.Inventory.Terminals = a.Terminals
	return h, a
}

func TestNativeTerminalSupportsMenusAndTextWithoutImplicitSubmission(t *testing.T) {
	for _, assistant := range []session.Assistant{session.AssistantClaude, session.AssistantCodex} {
		h, a := nativeTerminalActions()
		h.s.Assistant = assistant
		frame, err := a.NativeTerminal(context.Background(), "%1", nil)
		if err != nil || !session.ValidFingerprint(frame.Expect) || frame.Text != h.screen {
			t.Fatalf("%+v %v", frame, err)
		}
		for _, input := range []NativeTerminalInput{{Key: "down"}, {Key: "tab"}, {Text: "模型 filter"}, {Key: "backspace"}, {Key: "enter"}, {Key: "escape"}} {
			input.Expect = frame.Expect
			if _, err := a.NativeTerminal(context.Background(), "%1", &input); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.keys(); len(got) != 6 || got[2] != "模型 filter" || got[4] != "\r" {
			t.Fatalf("unexpected keys: %q", got)
		}
	}
}

func TestNativeTerminalRejectsMovedTargetsBeforeTyping(t *testing.T) {
	for _, change := range []string{"screen", "process", "unreadable", "unsupported", "revoked", "changed-during-permission-check"} {
		t.Run(change, func(t *testing.T) {
			h, a := nativeTerminalActions()
			ctx := context.Background()
			frame, err := a.NativeTerminal(ctx, "%1", nil)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "screen":
				h.screen = "different dialog"
			case "process":
				h.identity = "new process birth"
			case "unreadable":
				h.blind = true
			case "unsupported":
				h.s.Assistant = session.AssistantAntigravity
			case "revoked":
				ctx = WithOperationCheck(ctx, func() error { return Refusal{Code: "forbidden"} })
			case "changed-during-permission-check":
				ctx = WithOperationCheck(ctx, func() error { h.identity = "replacement"; return nil })
			}
			_, err = a.NativeTerminal(ctx, "%1", &NativeTerminalInput{Expect: frame.Expect, Key: "enter"})
			if err == nil || len(h.keys()) != 0 {
				t.Fatalf("err %v keys %q", err, h.keys())
			}
		})
	}
}

func TestNativeTerminalRejectsInjectedKeysAndNeverRetriesUnknownWrites(t *testing.T) {
	for _, input := range []NativeTerminalInput{{Text: "line\rcommand"}, {Text: "\x1b[A"}, {Key: "down", Text: "x"}, {Text: strings.Repeat("a", domainterminal.MaxInputBytes+1)}, {Key: "unknown"}, {Text: string([]byte{0xff})}} {
		input.Expect = strings.Repeat("a", 64)
		if _, err := (Actions{}).NativeTerminal(context.Background(), "%1", &input); err == nil {
			t.Fatalf("accepted %+v", input)
		}
	}
	h, a := nativeTerminalActions()
	frame, err := a.NativeTerminal(context.Background(), "%1", nil)
	if err != nil {
		t.Fatal(err)
	}
	h.writeError = errors.New("write outcome unknown")
	if _, err := a.NativeTerminal(context.Background(), "%1", &NativeTerminalInput{Expect: frame.Expect, Text: "value"}); err == nil {
		t.Fatal("unknown write succeeded")
	}
	if len(h.keys()) != 0 || h.attempts != 1 {
		t.Fatalf("keys %q attempts %d", h.keys(), h.attempts)
	}
}
