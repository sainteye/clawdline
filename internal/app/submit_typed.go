package app

import (
	"context"
	"fmt"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// KeyEnter is the key a page names to press Enter on a send that was typed
// and never submitted (`send_unsubmitted`). It is not in KeyName's list: it
// answers no menu, and it is pressed only at the words it names.
const KeyEnter = "enter"

// SubmitTyped presses Enter on a send the terminal took and never submitted.
//
// A send whose words were pasted but never seen arriving in the input line is
// refused as send_unsubmitted, and the words sit there (terminal.Unsubmitted).
// Until now the only way on was a person at the machine; from a phone, and on a
// Linux host with nobody at it, there was none. This is that Enter, and it is
// pressed only where it submits those words and nothing else:
//
//   - the screen is read first, inside the terminal's turn, and an unreadable
//     screen is input_unreadable with nothing pressed;
//   - a question on the screen is input_behind_question: an Enter at a picker
//     confirms whatever is highlighted, which nobody chose. The words may still
//     be in the input line under it, so the page offers the Enter again rather
//     than sending them again;
//   - an input line that is not an assistant's composer holding the words —
//     the end of `typed`, or a paste placeholder — is input_moved: the words
//     were submitted, cleared, or the assistant is gone and an Enter would run
//     them in a shell (terminal.HoldsTyped).
//
// `typed` is the send's text, or its end; a send of pictures alone names "".
// A nil error means Enter reached the tty. Whether the assistant took the turn
// is the transcript's to say.
func (a Actions) SubmitTyped(ctx context.Context, id, typed string) (session.Session, error) {
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	keys, ok := h.(ports.KeyHost)
	if !ok || a.Inventory.Screen == nil {
		return s, Refusal{Code: "backend_unsupported",
			Detail: fmt.Sprintf("nothing on this machine presses Enter in a %q session after reading it", s.Backend)}
	}
	release, err := a.turn(ctx, s)
	if err != nil {
		return s, err
	}
	defer release()
	note := map[string]any{"key": KeyEnter}
	screen, ok := a.Inventory.Screen.Capture(ctx, s)
	if !ok {
		note["verdict"] = "unreadable"
		a.record(ctx, "session.key", s.ID, note)
		return s, Refusal{Code: "input_unreadable",
			Detail: "That session's screen could not be read, so its input line could not be checked. Enter was not pressed."}
	}
	assistant := s.Assistant
	if assistant == "" {
		assistant = session.AssistantClaude
	}
	if _, menu := session.ReadMenu(screen, assistant, true); menu {
		note["verdict"] = "menu_up"
		a.record(ctx, "session.key", s.ID, note)
		return s, Refusal{Code: "input_behind_question",
			Detail: "That session is asking a question now, and Enter would answer it. Enter was not pressed."}
	}
	if !terminal.HoldsTyped(screen, typed) {
		note["verdict"] = "not_held"
		a.record(ctx, "session.key", s.ID, note)
		return s, Refusal{Code: "input_moved",
			Detail: "Those words are not in the session's input line now. Enter was not pressed."}
	}
	if err := keys.Keystroke(ctx, s, keyReturn); err != nil {
		note["failed"] = err.Error()
		a.record(ctx, "session.key", s.ID, note)
		return s, Refusal{Code: "terminal_io_failed",
			Detail: "The terminal command did not complete: " + err.Error()}
	}
	note["verdict"] = "pressed"
	a.record(ctx, "session.key", s.ID, note)
	return s, nil
}
