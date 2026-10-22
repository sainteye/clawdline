package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// antigravityAction is scoped to one caller-owned terminal lane. Every read
// and effect checks the same native process birth and conversation again.
// Cached screen state is never authority for a later key or paste.
type antigravityAction struct {
	actions Actions
	session session.Session
	keys    ports.KeyHost
	target  ports.AntigravityBinding
}

func (a Actions) antigravityAction(ctx context.Context, s session.Session, h ports.TerminalHost) (*antigravityAction, error) {
	keys, ok := h.(ports.KeyHost)
	if !ok || a.Inventory.Screen == nil || a.Antigravity == nil || s.ConversationID == "" || !s.Binding.Bound() {
		return nil, antigravityBindingRefusal()
	}
	b, err := a.Antigravity.AntigravityBinding(ctx, s)
	if err != nil || !b.Verified || b.Process.PID != s.PID || b.Process.Birth == "" || b.Process.Executable == "" || s.TTY == "" || b.Process.TTY != s.TTY || b.ConversationID != s.ConversationID ||
		(s.Backend == session.BackendTmux && b.Process.TerminalID != s.ID) || (!s.ExpectedStart.IsZero() && b.Process.Birth != s.ExpectedStart.UTC().Format(time.RFC3339Nano)) {
		return nil, antigravityBindingRefusal()
	}
	action := &antigravityAction{actions: a, session: s, keys: keys, target: b}
	return action, action.check(ctx)
}

func antigravityBindingRefusal() Refusal {
	return Refusal{Code: "provider_binding_unavailable", Detail: "Antigravity's current conversation could not be verified. No further input was sent."}
}
func (p *antigravityAction) check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		ref := antigravityBindingRefusal()
		ref.Cause = err
		return ref
	}
	if err := checkOperation(ctx); err != nil {
		if ref, ok := err.(Refusal); ok {
			return ref
		}
		return Refusal{Code: "forbidden", Detail: "This device may read, and not send.", Cause: err}
	}
	b, err := p.actions.Antigravity.AntigravityBinding(ctx, p.session)
	if err != nil || !b.Verified || b.Process != p.target.Process || b.ConversationID != p.target.ConversationID {
		return antigravityBindingRefusal()
	}
	if p.actions.AntigravityPolicy != nil {
		if err := p.actions.AntigravityPolicy(ctx, b); err != nil {
			ref := antigravityBindingRefusal()
			ref.Cause = err
			return ref
		}
	}
	return nil
}
func (p *antigravityAction) read(ctx context.Context) (string, error) {
	if err := p.check(ctx); err != nil {
		return "", err
	}
	screen, ok := p.actions.Inventory.Screen.Capture(ctx, p.session)
	if !ok {
		return "", Refusal{Code: "input_unreadable", Detail: "That session's screen could not be read, so its input line could not be checked. Enter was not pressed."}
	}
	if err := p.check(ctx); err != nil {
		return "", err
	}
	return screen, nil
}
func (p *antigravityAction) key(ctx context.Context, name string) error {
	var bytes []byte
	switch name {
	case "Up":
		bytes = []byte{0x1b, '[', 'A'}
	case "Down":
		bytes = []byte{0x1b, '[', 'B'}
	case "Enter":
		bytes = keyReturn
	case "Escape":
		bytes = []byte{0x1b}
	default:
		return antigravityOperationRefusal()
	}
	if err := p.check(ctx); err != nil {
		return err
	}
	return p.keys.Keystroke(ctx, p.session, bytes)
}
func antigravityOperationRefusal() Refusal {
	return Refusal{Code: "provider_operation_unsupported", Detail: "This operation is not available for Antigravity's current screen."}
}

// sendToHost is called only inside the caller's existing terminal lane.
func (a Actions) sendToHost(ctx context.Context, s session.Session, h ports.TerminalHost, text string) error {
	if s.Assistant != session.AssistantAntigravity {
		if err := h.Send(ctx, s, text); err != nil {
			return sendRefusal(s, err)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, terminal.SendConfirmationWindow())
	defer cancel()
	p, err := a.antigravityAction(ctx, s, h)
	if err != nil {
		return beforeTheFirstByte(err)
	}
	typer, ok := h.(typist)
	if !ok {
		return beforeTheFirstByte(antigravityOperationRefusal())
	}
	pasted, enterAttempted := false, false
	paste := func(ctx context.Context, text string) error {
		if err := p.check(ctx); err != nil {
			return err
		}
		err := typer.Type(ctx, s, text)
		// A failed paste may have written bytes. Only an explicit Unsent proves
		// otherwise; an uncertain send is never offered for automatic replay.
		pasted = !errors.As(err, new(terminal.Unsent))
		return err
	}
	// The screen must positively show its ready footer. A Stop sampled before
	// this operation cannot lend freshness to a later terminal read.
	fullyIdle := false
	key := func(ctx context.Context, name string) error {
		if err := p.check(ctx); err != nil {
			return err
		}
		enterAttempted = true
		return p.key(ctx, name)
	}
	err = terminal.SubmitAntigravityText(ctx, p.read, paste, key, text, fullyIdle, terminal.SendConfirmationWindow())
	if err == nil {
		return nil
	}
	if errors.Is(err, terminal.ErrAntigravityInputUnavailable) {
		return beforeTheFirstByte(Refusal{Code: "provider_input_unavailable", Detail: "Antigravity is not showing an empty, ready input line. No new message was submitted."})
	}
	if pasted && !enterAttempted {
		const detail = "Antigravity kept the pasted text without a confirmed submission. Refresh the Session before continuing."
		return Refusal{Code: "send_unsubmitted", Detail: detail, Cause: terminal.Unsubmitted{Why: detail}}
	}
	if ref, ok := err.(Refusal); ok && !pasted {
		return beforeTheFirstByte(ref)
	}
	return sendRefusal(s, err)
}

func (a Actions) keyAntigravity(ctx context.Context, s session.Session, h ports.TerminalHost, digit int, expect string) (session.Session, error) {
	if digit == 0 {
		return s, antigravityOperationRefusal()
	}
	if expect == "" {
		return s, Refusal{Code: "menu_unverified", Detail: "An Antigravity answer must name the question being answered."}
	}
	ctx, cancel := context.WithTimeout(ctx, terminal.SendConfirmationWindow())
	defer cancel()
	p, err := a.antigravityAction(ctx, s, h)
	if err != nil {
		return s, err
	}
	screen, err := p.read(ctx)
	if err != nil {
		return s, err
	}
	facts := session.ReadAntigravityScreen(screen, false)
	if facts.Kind != session.AntigravityMenu || session.MenuFingerprint(facts.Menu) != expect {
		return s, antigravityMenuMoved()
	}
	err = terminal.SubmitAntigravityMenu(ctx, p.read, p.key, facts.Revision, digit)
	if errors.Is(err, terminal.ErrAntigravityMenuChanged) {
		err = antigravityMenuMoved()
	}
	note := map[string]any{"key": digit, "expect": expect, "provider": "antigravity"}
	if err != nil {
		note["verdict"] = "unconfirmed"
		a.record(ctx, "session.key", s.ID, note)
		if ref, ok := err.(Refusal); ok {
			return s, ref
		}
		return s, Refusal{Code: "terminal_io_failed", Detail: "The Antigravity key could not be confirmed. Refresh the Session before trying again.", Cause: err}
	}
	note["verdict"] = "pressed"
	a.record(ctx, "session.key", s.ID, note)
	a.Inventory.Held.Pressed(s.ID)
	return s, nil
}
func antigravityMenuMoved() Refusal {
	return Refusal{Code: "menu_moved", Detail: "The question changed before confirmation. No answer was submitted."}
}

func (a Actions) submitAntigravityTyped(ctx context.Context, s session.Session, h ports.TerminalHost, typed string) (session.Session, error) {
	ctx, cancel := context.WithTimeout(ctx, terminal.SendConfirmationWindow())
	defer cancel()
	p, err := a.antigravityAction(ctx, s, h)
	if err != nil {
		return s, err
	}
	screen, err := p.read(ctx)
	if err != nil {
		return s, err
	}
	facts := session.ReadAntigravityScreen(screen, false)
	if facts.Kind != session.AntigravityDraft || !terminal.HoldsTyped(screen, typed) || strings.TrimSpace(typed) == "" || strings.HasPrefix(strings.TrimSpace(typed), "/") || strings.HasPrefix(strings.TrimSpace(typed), "!") {
		return s, Refusal{Code: "input_moved", Detail: "Those words are not in the session's input line now. Enter was not pressed."}
	}
	current, err := p.read(ctx)
	if err != nil {
		return s, err
	}
	now := session.ReadAntigravityScreen(current, false)
	if now.Kind != session.AntigravityDraft || now.Draft != facts.Draft || !terminal.HoldsTyped(current, typed) {
		return s, Refusal{Code: "input_moved", Detail: "Those words are not in the session's input line now. Enter was not pressed."}
	}
	if err := p.key(ctx, "Enter"); err != nil {
		return s, Refusal{Code: "terminal_io_failed", Detail: "The Antigravity key could not be confirmed. Refresh the Session before trying again.", Cause: err}
	}
	a.Inventory.Held.Pressed(s.ID)
	return s, nil
}

func (a Actions) interruptAntigravity(ctx context.Context, s session.Session, h ports.TerminalHost) error {
	ctx, cancel := context.WithTimeout(ctx, terminal.SendConfirmationWindow())
	defer cancel()
	p, err := a.antigravityAction(ctx, s, h)
	if err != nil {
		return err
	}
	screen, err := p.read(ctx)
	if err != nil {
		return err
	}
	facts := session.ReadAntigravityScreen(screen, false)
	if facts.Kind != session.AntigravityWorking && facts.Kind != session.AntigravityMenu {
		return antigravityOperationRefusal()
	}
	if err := p.key(ctx, "Escape"); err != nil {
		if ref, ok := err.(Refusal); ok {
			return ref
		}
		return Refusal{Code: "terminal_io_failed", Detail: "The Antigravity key could not be confirmed. Refresh the Session before trying again.", Cause: err}
	}
	return nil
}
