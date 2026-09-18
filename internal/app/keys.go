package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sainteye/clawdline-go/internal/app/ports"
	"github.com/sainteye/clawdline-go/internal/domain/session"
)

// The keys the menu-answer route may send (TerminalMenuAnswerPolicy). A byte
// channel into a tty is an escape-sequence channel into a tty, so the set is
// closed: a digit answers a menu and can do nothing else, Tab moves a
// multi-select's focus, and back-tab is the one sequence, which Claude Code
// uses to cycle permission modes. `submit` is not a key but an act — pressing a
// multi-select's button — and the walk to it happens here, reading the screen
// back before each step.
var (
	keyTab     = []byte{0x09}
	keyBackTab = []byte{0x1b, 0x5b, 0x5a}
	keyReturn  = []byte{0x0d}
)

// KeyName is the allowlist, parsed before anything is looked up.
func KeyName(key string) (bytes []byte, digit int, ok bool) {
	switch key {
	case "submit":
		return nil, 0, true
	case "tab":
		return keyTab, 0, true
	case "shift+tab":
		return keyBackTab, 0, true
	}
	if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
		return []byte{key[0]}, int(key[0] - '0'), true
	}
	return nil, 0, false
}

// Key answers the menu on a session's screen, or sends one of the keys this
// daemon names (the Swift app's `POST /key`).
//
// Not Send: Send wraps its text in a bracketed paste and follows it with a
// Return, and a picker throws the paste away and acts on the Return — "Tea"
// answered "Water". A digit outside a paste is the only thing that answers the
// question that was actually asked.
//
// A nil error means the keystrokes reached the tty. Whether the question was
// answered is the fleet list's to say, as for every other action.
func (a Actions) Key(ctx context.Context, id, key string) (session.Session, error) {
	bytes, digit, ok := KeyName(key)
	if !ok {
		return session.Session{}, Refusal{Code: "bad_request",
			Detail: `key must be "1"…"9", "tab", "shift+tab" or "submit".`}
	}
	s, err := a.Find(ctx, id)
	if err != nil {
		return session.Session{}, err
	}
	h, err := a.host(s)
	if err != nil {
		return session.Session{}, err
	}
	keys, ok := h.(ports.KeyHost)
	if !ok {
		return s, Refusal{Code: "backend_unsupported",
			Detail: fmt.Sprintf("nothing on this machine types keys into a %q session", s.Backend)}
	}
	// Answering a menu is several keystrokes read back one at a time; it is
	// one write to the terminal, taken as one turn.
	release, err := a.turn(ctx, s)
	if err != nil {
		return s, err
	}
	defer release()
	p := presser{keys: keys, screen: a.Inventory.Screen, s: s, note: map[string]any{"key": key}}
	switch {
	case key == "submit":
		err = p.submitMenu(ctx)
	case digit > 0:
		err = p.answer(ctx, digit, bytes)
	default:
		err = p.press(ctx, bytes)
	}
	if err != nil {
		p.note["failed"] = err.Error()
		a.record(ctx, "session.key", s.ID, p.note)
		return s, Refusal{Code: "terminal_io_failed",
			Detail: "The terminal command did not complete: " + err.Error()}
	}
	a.record(ctx, "session.key", s.ID, p.note)
	return s, nil
}

// presser is one answer in progress. `note` is its own account of what it
// decided, recorded with the event: the whole effect of an answer happens on
// somebody else's screen, and without this a tap that reached the tty and
// stopped there is indistinguishable from one that answered.
type presser struct {
	keys   ports.KeyHost
	screen ports.ScreenHost
	s      session.Session
	note   map[string]any
}

func (p presser) press(ctx context.Context, bytes []byte) error {
	return p.keys.Keystroke(ctx, p.s, bytes)
}

// read is the menu on screen now. The gate is open: this path only exists
// behind a menu the page already drew buttons for.
func (p presser) read(ctx context.Context) (session.Menu, bool) {
	if p.screen == nil {
		return session.Menu{}, false
	}
	screen, ok := p.screen.Capture(ctx, p.s)
	if !ok {
		return session.Menu{}, false
	}
	assistant := p.s.Assistant
	if assistant == "" {
		assistant = session.AssistantClaude
	}
	return session.ReadMenu(screen, assistant, true)
}

// answer is Targets.answer: read which kind of picker this is before typing at
// it, type the digit, and confirm only what the screen shows landed.
//
// A dialog drawn without numbers has numeric selection switched off, so the
// digit would fall through into the composer; that one is answered by walking
// the highlight. A capture that fails takes the numbered path, which is what a
// dialog somebody is looking at right now almost always is.
func (p presser) answer(ctx context.Context, want int, bytes []byte) error {
	seen, read := p.read(ctx)
	p.note["read"] = read
	var asked *session.Menu
	if read {
		asked = &seen
		p.note["numbered"] = seen.Numbered
		p.note["steps"] = len(seen.Steps)
		p.note["submit"] = seen.Submit != nil
		if seen.Selected != nil {
			p.note["caret"] = *seen.Selected
		}
	}
	if read && !seen.Numbered {
		return p.highlight(ctx, want, seen)
	}
	if err := p.press(ctx, bytes); err != nil {
		return err
	}
	// A multi-select's digit toggles its row, and a Return there is a second
	// press of the same row: the rows are sent by the button, not by this.
	if read && seen.Submit != nil {
		p.note["verdict"] = "toggled"
		return nil
	}
	return p.confirm(ctx, want, asked)
}

// highlight answers a picker that takes no digits by walking its highlight
// with j/k and confirming there. If the reading was wrong the highlight never
// lands and no Return is sent: a button that did nothing, not an answer
// nobody chose.
func (p presser) highlight(ctx context.Context, want int, menu session.Menu) error {
	if menu.Selected == nil {
		p.note["verdict"] = "no_caret"
		return nil
	}
	key, times := session.Walk(*menu.Selected, want)
	for i := 0; i < times; i++ {
		if err := p.press(ctx, []byte{key}); err != nil {
			return err
		}
	}
	return p.confirm(ctx, want, &menu)
}

// confirm is Targets.confirmSelection: Return only once the screen shows the
// digit landed on the wanted row of the same question. Two reads, because a
// terminal repaints on its own schedule. No menu on screen is either an answer
// that closed the picker or an unreadable screen; neither is a reason to type.
func (p presser) confirm(ctx context.Context, want int, asked *session.Menu) error {
	for attempt := 0; attempt < 2; attempt++ {
		wait := 120 * time.Millisecond
		if attempt > 0 {
			wait = 250 * time.Millisecond
		}
		if err := sleep(ctx, wait); err != nil {
			return err
		}
		now, ok := p.read(ctx)
		if !ok {
			p.note[fmt.Sprintf("confirm%d", attempt)] = "no_menu"
			continue
		}
		verdict := session.Confirm(want, asked, now)
		p.note[fmt.Sprintf("confirm%d", attempt)] = string(verdict)
		switch verdict {
		case session.ConfirmSend:
			p.note["verdict"] = "returned"
			return p.press(ctx, keyReturn)
		case session.ConfirmMovedOn:
			p.note["verdict"] = "moved_on"
			return nil
		}
	}
	p.note["verdict"] = "no_return"
	return nil
}

// submitMenu is Targets.submitMenu: press the button under a multi-select's
// rows. Its digits toggle and Return on a row toggles too, so this walks the
// focus with Tab — which inside the dialog's own text box is one of the few
// keys passed through, and which does nothing once it is on the button — and
// reads the screen before every step, a bounded number of times.
//
// **Deliberately different from the Swift app in one place.** In a set of
// questions, Claude Code v2.1.274 takes Tab as "next question": one Tab from a
// multi-select's rows put the review screen up, with the ticks kept — which is
// what pressing its button does. The Swift walk read that screen, found no
// button, and reported a failure for a press that had worked (measured
// 2026-09-17 on a disposable session). So a walk whose Tab moved the picker on
// — the tab bar or the question changed, the rule `session.Confirm` applies to
// a digit — is a finished press here, not a refusal.
func (p presser) submitMenu(ctx context.Context) error {
	var first *session.Menu
	for steps := 0; ; steps++ {
		menu, ok := p.read(ctx)
		if !ok {
			return errors.New("Could not read that session's screen.")
		}
		if first == nil {
			first = &menu
		} else if session.Confirm(0, first, menu) == session.ConfirmMovedOn {
			p.note["steps"] = steps
			p.note["verdict"] = "moved_on"
			return nil
		}
		if menu.Submit == nil {
			return errors.New("That question has no Submit to press.")
		}
		if menu.Submit.Selected {
			p.note["steps"] = steps
			return p.press(ctx, keyReturn)
		}
		if steps > len(menu.Options)+1 {
			return errors.New("The highlight would not move onto Submit.")
		}
		if err := p.press(ctx, keyTab); err != nil {
			return err
		}
		if err := sleep(ctx, 120*time.Millisecond); err != nil {
			return err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
