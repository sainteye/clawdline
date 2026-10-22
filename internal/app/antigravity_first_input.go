package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

// SendAntigravityFirst is the only UUID-free input path. It admits one private
// prepared launch, not an arbitrary inventory pane. Native binding and loaded
// context remain separate positive observations after this method returns.
func (a Actions) SendAntigravityFirst(ctx context.Context, launchID, requestID, text string) (session.Session, error) {
	unavailable := func(cause error) error {
		return beforeTheFirstByte(Refusal{Code: "first_input_unavailable", Detail: "Antigravity's prepared launch could not be verified. No input was sent.", Cause: cause})
	}
	if a.AntigravityFirstInput == nil || a.Inventory.Screen == nil || text == "" {
		return session.Session{}, unavailable(nil)
	}
	ctx, cancel := context.WithTimeout(ports.Asked(ctx), terminal.SendConfirmationWindow())
	defer cancel()
	target, err := a.AntigravityFirstInput.Target(ctx, launchID)
	if err != nil {
		return session.Session{}, unavailable(err)
	}
	p := target.Process
	birth, err := time.Parse(time.RFC3339Nano, p.Birth)
	if err != nil || birth.IsZero() || birth.UTC().Format(time.RFC3339Nano) != p.Birth || target.LaunchID != launchID ||
		p.PID <= 0 || p.ParentPID <= 0 || p.Executable == "" || p.KernelGeneration == "" || p.TTY == "" || strings.HasPrefix(p.TTY, "/dev/") || p.TerminalID == "" {
		return session.Session{}, unavailable(nil)
	}
	s := session.Session{ID: p.TerminalID, Backend: session.Backend(target.Backend), Assistant: session.AssistantAntigravity, PID: p.PID, TTY: p.TTY, ExpectedStart: birth}
	host, err := a.host(ctx, s)
	if err != nil {
		return s, unavailable(err)
	}
	input, ok := host.(ports.AntigravityFirstInputTerminal)
	if !ok {
		return s, unavailable(nil)
	}
	release, err := a.turn(ctx, s)
	if err != nil {
		return s, unavailable(err)
	}
	defer release()
	var attempt ports.AntigravityFirstInputAttempt
	check := func(c context.Context) error {
		if err := c.Err(); err != nil {
			return err
		}
		if err := checkOperation(c); err != nil {
			return err
		}
		if err := a.AntigravityFirstInput.Check(c, target); err != nil {
			return err
		}
		if attempt != nil {
			return attempt.Check(c)
		}
		return nil
	}
	last := ""
	read := func(c context.Context) (string, error) {
		if err := check(c); err != nil {
			return "", err
		}
		screen, ok := a.Inventory.Screen.Capture(c, s)
		if !ok {
			return "", terminal.ErrAntigravityInputUnavailable
		}
		if err := check(c); err != nil {
			return "", err
		}
		last = screen
		return screen, nil
	}
	before, err := read(ctx)
	if err != nil {
		return s, unavailable(err)
	}
	if !session.AntigravitySendReady(before, text, false) {
		return s, beforeTheFirstByte(Refusal{Code: "provider_input_unavailable", Detail: "Antigravity is not showing an empty, ready input line. No new message was submitted."})
	}
	pending := func(cause error) error {
		return Refusal{Code: "first_input_pending", Detail: "The first-input attempt could not be confirmed. Refresh the Session; it will not be retried automatically.", Cause: cause}
	}
	attempt, err = a.AntigravityFirstInput.Reserve(ctx, target, requestID, fmt.Sprintf("%x", sha256.Sum256([]byte(text))))
	if err != nil {
		return s, pending(err)
	}
	if attempt == nil {
		return s, Refusal{Code: "first_input_consumed", Detail: "This Antigravity launch has already reserved its first input. Refresh the Session before continuing."}
	}
	paste := func(c context.Context, value string) error {
		if err := check(c); err != nil {
			return err
		}
		if err := attempt.BeforePaste(c); err != nil {
			return err
		}
		return input.TypeAntigravityFirst(c, s, value, check)
	}
	key := func(c context.Context, name string) error {
		if name != "Enter" {
			return terminal.ErrAntigravityInputUnavailable
		}
		prior := session.ReadAntigravityScreen(last, false)
		if err := check(c); err != nil {
			return err
		}
		if err := attempt.BeforeEnter(c); err != nil {
			return err
		}
		finalCheck := func(c context.Context) error {
			current, err := read(c)
			if err != nil {
				return err
			}
			now := session.ReadAntigravityScreen(current, false)
			if prior.Kind != session.AntigravityDraft || now.Kind != session.AntigravityDraft || prior.Draft != now.Draft || !terminal.HoldsTyped(current, text) {
				return terminal.ErrAntigravityInputUnavailable
			}
			return nil
		}
		return input.EnterAntigravityFirst(c, s, finalCheck)
	}
	if err = terminal.SubmitAntigravityText(ctx, read, paste, key, text, false, terminal.SendConfirmationWindow()); err != nil {
		return s, pending(err)
	}
	return s, nil // one terminal Enter acknowledgement, not provider delivery
}
