package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"unicode"
	"unicode/utf8"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
	domainterminal "github.com/sainteye/clawdline/internal/domain/terminal"
)

// NativeTerminalFrame binds the visible CLI to the exact foreground process.
// It is not a shell, a new terminal, or a prompt sent to a model.
type NativeTerminalFrame struct {
	Text   string `json:"text"`
	Expect string `json:"expect"`
}

type NativeTerminalInput struct {
	Expect string `json:"expect"`
	Key    string `json:"key,omitempty"`
	Text   string `json:"text,omitempty"`
}

type nativeTerminalHost interface {
	NativeTerminalIdentity(context.Context, session.Session) (string, error)
	Keystroke(context.Context, session.Session, []byte) error
}

// Named terminal keys are separate from the menu-answer route's closed list.
// Text is a single UTF-8 line and cannot smuggle keys or submission sequences.
func nativeTerminalBytes(input NativeTerminalInput) ([]byte, bool) {
	if input.Key != "" {
		if input.Text != "" {
			return nil, false
		}
		keys := map[string]string{"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
			"enter": "\r", "escape": "\x1b", "tab": "\t", "shift+tab": "\x1b[Z", "backspace": "\x7f",
			"delete": "\x1b[3~", "home": "\x1b[H", "end": "\x1b[F", "pageup": "\x1b[5~", "pagedown": "\x1b[6~"}
		text, ok := keys[input.Key]
		return []byte(text), ok
	}
	if input.Text == "" || len(input.Text) > domainterminal.MaxInputBytes || !utf8.ValidString(input.Text) {
		return nil, false
	}
	for _, r := range input.Text {
		if unicode.IsControl(r) {
			return nil, false
		}
	}
	return []byte(input.Text), true
}

func nativeTerminalRefusal(code string) error {
	return beforeTheFirstByte(Refusal{Code: code, RawDetail: true, Detail: code})
}

func (a Actions) nativeTerminalFrame(ctx context.Context, s session.Session, h nativeTerminalHost) (NativeTerminalFrame, error) {
	identity, err := h.NativeTerminalIdentity(ctx, s)
	if err != nil || identity == "" {
		return NativeTerminalFrame{}, nativeTerminalRefusal("input_moved")
	}
	if a.Inventory.Screen == nil {
		return NativeTerminalFrame{}, nativeTerminalRefusal("input_unreadable")
	}
	text, ok := a.Inventory.Screen.Capture(ctx, s)
	if !ok {
		return NativeTerminalFrame{}, nativeTerminalRefusal("input_unreadable")
	}
	after, err := h.NativeTerminalIdentity(ctx, s)
	if err != nil || after != identity {
		return NativeTerminalFrame{}, nativeTerminalRefusal("input_moved")
	}
	return NativeTerminalFrame{Text: text, Expect: fmt.Sprintf("%x", sha256.Sum256([]byte(identity+"\x00"+text)))}, nil
}

// NativeTerminal reads, or types one explicitly requested key/text batch into,
// this Session's existing CLI. It never retries, adds Enter, starts a shell or
// falls back to another process after the assistant leaves its terminal.
func (a Actions) NativeTerminal(ctx context.Context, id string, input *NativeTerminalInput) (NativeTerminalFrame, error) {
	var bytes []byte
	if input != nil {
		var ok bool
		bytes, ok = nativeTerminalBytes(*input)
		if !ok || !session.ValidFingerprint(input.Expect) {
			return NativeTerminalFrame{}, nativeTerminalRefusal("bad_request")
		}
	}
	ctx = ports.Asked(ctx)
	s, err := a.Find(ctx, id)
	if err != nil {
		return NativeTerminalFrame{}, beforeTheFirstByte(err)
	}
	if s.Assistant != session.AssistantClaude && s.Assistant != session.AssistantCodex {
		return NativeTerminalFrame{}, nativeTerminalRefusal("backend_unsupported")
	}
	host, err := a.host(ctx, s)
	if err != nil {
		return NativeTerminalFrame{}, beforeTheFirstByte(err)
	}
	h, ok := host.(nativeTerminalHost)
	if !ok {
		return NativeTerminalFrame{}, nativeTerminalRefusal("backend_unsupported")
	}
	release, err := a.turn(ctx, s)
	if err != nil {
		return NativeTerminalFrame{}, err
	}
	defer release()
	frame, err := a.nativeTerminalFrame(ctx, s, h)
	if err != nil || input == nil {
		return frame, err
	}
	if frame.Expect != input.Expect {
		return frame, nativeTerminalRefusal("input_moved")
	}
	if err := checkOperation(ctx); err != nil {
		return NativeTerminalFrame{}, beforeTheFirstByte(err)
	}
	// Permission checking may have waited. Verify the process and screen again
	// inside the same send lane before the first byte can be written.
	latest, err := a.nativeTerminalFrame(ctx, s, h)
	if err != nil {
		return NativeTerminalFrame{}, err
	}
	if latest.Expect != frame.Expect {
		return latest, nativeTerminalRefusal("input_moved")
	}
	if err := h.Keystroke(ctx, s, bytes); err != nil {
		return NativeTerminalFrame{}, sendRefusal(s, err)
	}
	a.Inventory.Held.Pressed(s.ID)
	return NativeTerminalFrame{}, nil
}
