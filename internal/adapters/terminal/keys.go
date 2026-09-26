package terminal

import (
	"context"
	"encoding/hex"
	"errors"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// Keystroke types raw key bytes into a pane as one keypress (Tmux.keystroke).
//
// Outside any bracketed paste on purpose: a picker throws a paste away and acts
// on the Return that follows it, while a bare digit is a selection. `-H` takes
// hex, so tmux does not look for key names in the bytes, and one `send-keys`
// keeps a multi-byte sequence together where separate calls would leave room
// for somebody else's input between them.
func (t *Tmux) Keystroke(ctx context.Context, s session.Session, bytes []byte) error {
	if len(bytes) == 0 {
		return errors.New("there is no key to send")
	}
	args := []string{"send-keys", "-t", s.ID, "-H"}
	for _, b := range bytes {
		args = append(args, hex.EncodeToString([]byte{b}))
	}
	return t.run(ctx, args...)
}

var _ ports.KeyHost = (*Tmux)(nil)

// keyEscape is what Interrupt types on every backend.
var keyEscape = []byte{0x1b}

// Screens reads a session's visible screen through whichever backend owns it:
// tmux on every platform, iTerm2 where there is one. A backend with no way to
// read its screen answers false, which callers treat as "unread", not "empty".
type Screens struct{ hosts []ports.TerminalHost }

func NewScreens() Screens { return Screens{hosts: Hosts()} }

func (s Screens) Capture(ctx context.Context, sess session.Session) (string, bool) {
	for _, h := range s.hosts {
		if h.Name() != string(sess.Backend) {
			continue
		}
		if reader, ok := h.(ports.ScreenHost); ok {
			return reader.Capture(ctx, sess)
		}
	}
	return "", false
}

var _ ports.ScreenHost = Screens{}
