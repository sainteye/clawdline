package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sainteye/clawdline/internal/adapters/terminal"
	"github.com/sainteye/clawdline/internal/app/lane"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type agyActionBinding func(context.Context, session.Session) (ports.AntigravityBinding, error)

func (f agyActionBinding) AntigravityBinding(c context.Context, s session.Session) (ports.AntigravityBinding, error) {
	return f(c, s)
}

type agyActionPane struct {
	*menuTerminal
	pasted     []string
	rawSends   int
	keyError   error
	afterPaste func()
}

func (p *agyActionPane) Send(context.Context, session.Session, string) error {
	p.rawSends++
	return nil
}
func (p *agyActionPane) Type(_ context.Context, _ session.Session, text string) error {
	p.pasted = append(p.pasted, text)
	p.show(agyActionComposer(text))
	if p.afterPaste != nil {
		p.afterPaste()
	}
	return nil
}
func agyActionComposer(text string) string {
	return "Antigravity CLI 1.4.5\n────────────────────────\n> " + text + "\n────────────────────────\n? for shortcuts                  Gemini 3.8 Flash · high"
}
func agyActionPermission(selected int) string {
	var b strings.Builder
	b.WriteString("Command\n────────────────────────\nRequesting permission for:\n   printf SAMPLE\n\nRun this command?\n")
	for i, label := range []string{"Yes, run command", "Yes, and always allow in this conversation for commands that start with 'printf'", "Yes, and always allow for commands that start with 'printf' (Persist to settings.json)", "No, cancel"} {
		prefix := "  "
		if i+1 == selected {
			prefix = "> "
		}
		fmt.Fprintf(&b, "%s%d. %s\n", prefix, i+1, label)
	}
	b.WriteString("\n  ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command\nesc to cancel                     Gemini 3.8 Flash · high")
	return b.String()
}
func agyActions() (Actions, *agyActionPane, *ports.AntigravityBinding) {
	started := time.Unix(1000, 123000).UTC()
	s := session.Session{ID: "%1", Backend: session.BackendTmux, Assistant: session.AssistantAntigravity, PID: 101, TTY: "ttySynthetic", ExpectedStart: started, ConversationID: "74000000-0000-4000-8000-000000000001", Binding: session.BindingLaunchHook, State: session.StateIdle}
	p := &agyActionPane{menuTerminal: &menuTerminal{s: s, screen: agyActionComposer("")}}
	binding := &ports.AntigravityBinding{Process: ports.AntigravityProcess{PID: s.PID, Birth: started.Format(time.RFC3339Nano), TTY: s.TTY, TerminalID: s.ID, Executable: "/synthetic/agy"}, ConversationID: s.ConversationID, Verified: true, StateFresh: true, State: session.StateIdle, Event: "Stop"}
	a := Actions{Terminals: []ports.TerminalHost{p}, Inventory: Inventory{Terminals: []ports.TerminalHost{p}, Screen: p}, Lanes: lane.New(lane.DefaultLimit), Antigravity: agyActionBinding(func(context.Context, session.Session) (ports.AntigravityBinding, error) { return *binding, nil })}
	return a, p, binding
}
func TestAntigravityActionsPasteOnceAndPressOneEnter(t *testing.T) {
	a, p, _ := agyActions()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := a.send(ctx, p.s, "hello"); err != nil {
		t.Fatal(err)
	}
	if p.rawSends != 0 || len(p.pasted) != 1 || strings.Join(p.keys(), "") != "\r" {
		t.Fatalf("raw=%d pasted=%q keys=%q", p.rawSends, p.pasted, p.keys())
	}
}
func TestAntigravityActionsRefuseUnverifiedBindingAndDraft(t *testing.T) {
	for _, kind := range []string{"missing", "unverified", "other-conversation", "draft", "slash", "shell"} {
		t.Run(kind, func(t *testing.T) {
			a, p, b := agyActions()
			text := "hello"
			switch kind {
			case "missing":
				a.Antigravity = nil
			case "unverified":
				b.Verified = false
			case "other-conversation":
				b.ConversationID = "74000000-0000-4000-8000-000000000002"
			case "draft":
				p.show(agyActionComposer("mine"))
			case "slash":
				text = "/config"
			case "shell":
				text = "! touch file"
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := a.send(ctx, p.s, text); err == nil {
				t.Fatal("unsafe send admitted")
			}
			if p.rawSends != 0 || len(p.pasted) != 0 || len(p.keys()) != 0 {
				t.Fatal("refused input wrote", p.pasted, p.keys(), p.rawSends)
			}
		})
	}
}
func TestAntigravityActionsRecheckIdentityAndAuthorizationAfterPaste(t *testing.T) {
	for _, kind := range []string{"birth", "conversation", "authorization"} {
		t.Run(kind, func(t *testing.T) {
			a, p, b := agyActions()
			revoked := false
			p.afterPaste = func() {
				switch kind {
				case "birth":
					b.Process.Birth = "changed"
				case "conversation":
					b.ConversationID = "74000000-0000-4000-8000-000000000002"
				case "authorization":
					revoked = true
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			ctx = WithOperationCheck(ctx, func() error {
				if revoked {
					return errors.New("revoked")
				}
				return nil
			})
			if _, err := a.send(ctx, p.s, "hello"); err == nil {
				t.Fatal("changed target admitted")
			}
			if len(p.pasted) != 1 || len(p.keys()) != 0 {
				t.Fatalf("paste=%q keys=%q", p.pasted, p.keys())
			}
		})
	}
}
func TestAntigravityActionsAnswerWithArrowsAtTheNamedQuestion(t *testing.T) {
	a, p, _ := agyActions()
	p.show(agyActionPermission(1))
	selected := 1
	p.onKey = func(p *menuTerminal, k []byte) {
		switch string(k) {
		case "\x1b[B":
			selected++
			p.show(agyActionPermission(selected))
		case "\x1b[A":
			selected--
			p.show(agyActionPermission(selected))
		}
	}
	expect := session.MenuFingerprint(session.ReadAntigravityScreen(agyActionPermission(1), false).Menu)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := a.Key(ctx, p.s.ID, "2", expect); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.keys(), ""); got != "\x1b[B\r" {
		t.Fatalf("keys %q", got)
	}
}
func TestAntigravityActionsNeverAnswerAnUnnamedOrChangedMenu(t *testing.T) {
	for _, kind := range []string{"missing", "wrong", "changed-after-arrow", "binding"} {
		t.Run(kind, func(t *testing.T) {
			a, p, b := agyActions()
			p.show(agyActionPermission(1))
			expect := session.MenuFingerprint(session.ReadAntigravityScreen(agyActionPermission(1), false).Menu)
			switch kind {
			case "missing":
				expect = ""
			case "wrong":
				expect = strings.Repeat("0", 64)
			case "binding":
				b.Verified = false
			case "changed-after-arrow":
				p.onKey = func(p *menuTerminal, _ []byte) {
					p.show(strings.ReplaceAll(agyActionPermission(2), "SAMPLE", "DIFFERENT"))
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if _, err := a.Key(ctx, p.s.ID, "2", expect); err == nil {
				t.Fatal("unsafe answer admitted")
			}
			keys := strings.Join(p.keys(), "")
			if strings.Contains(keys, "\r") || strings.Contains(keys, "2") {
				t.Fatalf("unexpected confirmation %q", keys)
			}
		})
	}
}

func TestAntigravityActionsRecoverOnlyTheCurrentPlainDraft(t *testing.T) {
	for _, kind := range []string{"plain", "menu", "slash", "shell", "binding", "other-draft"} {
		t.Run(kind, func(t *testing.T) {
			a, p, b := agyActions()
			typed := "hello"
			p.show(agyActionComposer(typed))
			switch kind {
			case "menu":
				p.show(agyActionPermission(1))
			case "slash":
				typed = "/config"
				p.show(agyActionComposer(typed))
			case "shell":
				typed = "! touch file"
				p.show(agyActionComposer(typed))
			case "binding":
				b.Verified = false
			case "other-draft":
				p.show(agyActionComposer("different"))
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := a.SubmitTyped(ctx, p.s.ID, typed)
			if kind == "plain" {
				if err != nil || strings.Join(p.keys(), "") != "\r" {
					t.Fatalf("err=%v keys=%q", err, p.keys())
				}
			} else if err == nil || len(p.keys()) != 0 {
				t.Fatalf("unsafe recovery err=%v keys=%q", err, p.keys())
			}
		})
	}
}
func TestAntigravityActionsDoNotRunForeignComposerPreparation(t *testing.T) {
	a, p, _ := agyActions()
	prepared := false
	_, err := a.SendPrepared(context.Background(), p.s.ID, "hello", func(context.Context, func([]byte) error, func() (string, bool)) error { prepared = true; return nil })
	if err == nil || prepared || len(p.keys()) != 0 || p.rawSends != 0 {
		t.Fatal("foreign preparation ran", err, prepared, p.keys(), p.rawSends)
	}
}
func TestAntigravityActionsInterruptOnlyARecognizedLiveSurface(t *testing.T) {
	for _, kind := range []string{"working", "permission", "idle", "unknown", "binding"} {
		t.Run(kind, func(t *testing.T) {
			a, p, b := agyActions()
			switch kind {
			case "working":
				p.show("⠋ Generating...\n────────────────────────\n> \n────────────────────────\nesc to cancel                     Gemini 3.8 Flash · high")
			case "permission":
				p.show(agyActionPermission(1))
			case "unknown":
				p.show("> shell")
			case "binding":
				b.Verified = false
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := a.Interrupt(ctx, p.s.ID)
			if kind == "working" || kind == "permission" {
				if err != nil || strings.Join(p.keys(), "") != "\x1b" {
					t.Fatalf("err=%v keys=%q", err, p.keys())
				}
			} else if err == nil || len(p.keys()) != 0 {
				t.Fatalf("unsafe interrupt err=%v keys=%q", err, p.keys())
			}
		})
	}
}
func TestAntigravityActionsAfterPasteFailureNeverAllowsAutomaticReplay(t *testing.T) {
	a, p, b := agyActions()
	p.afterPaste = func() { b.Verified = false }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := a.send(ctx, p.s, "hello")
	var unsubmitted terminal.Unsubmitted
	var unsent terminal.Unsent
	if !errors.As(err, &unsubmitted) || errors.As(err, &unsent) || len(p.pasted) != 1 || len(p.keys()) != 0 {
		t.Fatalf("err=%v pasted=%q keys=%q", err, p.pasted, p.keys())
	}
}

func (p *agyActionPane) Keystroke(ctx context.Context, s session.Session, b []byte) error {
	if err := p.menuTerminal.Keystroke(ctx, s, b); err != nil {
		return err
	}
	return p.keyError
}
func TestAntigravityActionsLostEnterReplyRemainsUncertain(t *testing.T) {
	a, p, _ := agyActions()
	p.keyError = errors.New("lost acknowledgment")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := a.send(ctx, p.s, "hello")
	var ref Refusal
	var unsent terminal.Unsent
	var draft terminal.Unsubmitted
	if !errors.As(err, &ref) || ref.Code != "send_failed" || errors.As(err, &unsent) || errors.As(err, &draft) || strings.Join(p.keys(), "") != "\r" || len(p.pasted) != 1 {
		t.Fatalf("err=%v pasted=%q keys=%q", err, p.pasted, p.keys())
	}
}

func TestAntigravityBoundActionsRecheckPolicyBeforeEachEffect(t *testing.T) {
	for _, kind := range []string{"before-paste", "after-paste", "menu"} {
		t.Run(kind, func(t *testing.T) {
			a, pane, _ := agyActions()
			revoked := kind == "before-paste"
			checks := 0
			a.AntigravityPolicy = func(context.Context, ports.AntigravityBinding) error {
				checks++
				if revoked {
					return errors.New("synthetic policy revoked")
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if kind == "menu" {
				pane.show(agyActionPermission(1))
				expected := session.MenuFingerprint(session.ReadAntigravityScreen(agyActionPermission(1), false).Menu)
				pane.onKey = func(p *menuTerminal, _ []byte) { revoked = true; p.show(agyActionPermission(2)) }
				if _, err := a.Key(ctx, pane.s.ID, "2", expected); err == nil {
					t.Fatal("revoked menu confirmed")
				}
				if strings.Contains(strings.Join(pane.keys(), ""), "\r") {
					t.Fatal("policy revoked after navigation but Enter sent")
				}
			} else {
				pane.afterPaste = func() { revoked = true }
				if _, err := a.send(ctx, pane.s, "synthetic input"); err == nil {
					t.Fatal("revoked policy submitted")
				}
				if pane.rawSends != 0 || len(pane.keys()) != 0 {
					t.Fatal("revoked policy fell back to generic send")
				}
				if kind == "before-paste" && len(pane.pasted) != 0 {
					t.Fatal("initially revoked policy pasted")
				}
				if kind == "after-paste" && len(pane.pasted) != 1 {
					t.Fatal("retained draft lost")
				}
			}
			if checks == 0 {
				t.Fatal("bound action did not consult policy")
			}
		})
	}
}
