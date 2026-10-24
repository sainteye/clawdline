package app

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sainteye/clawdline/internal/adapters/antigravitylaunch"
	"github.com/sainteye/clawdline/internal/adapters/projects"
	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type firstInputTestPort struct {
	owner       *antigravitylaunch.Store
	target      ports.AntigravityFirstInputTarget
	check       func() error
	beforeEnter func()
}

func (p *firstInputTestPort) Target(context.Context, string) (ports.AntigravityFirstInputTarget, error) {
	return p.target, nil
}
func (p *firstInputTestPort) Check(_ context.Context, target ports.AntigravityFirstInputTarget) error {
	if target != p.target {
		return errors.New("synthetic process changed")
	}
	if p.check != nil {
		return p.check()
	}
	return nil
}
func (p *firstInputTestPort) Reserve(c context.Context, target ports.AntigravityFirstInputTarget, key, digest string) (ports.AntigravityFirstInputAttempt, error) {
	_, a, err := p.owner.ReserveFirstInput(c, target.LaunchID, key, digest, target.Process)
	if err != nil || a == nil {
		return nil, err
	}
	return firstInputTestAttempt{FirstInputAttempt: a, beforeEnter: p.beforeEnter}, nil
}

type firstInputTestAttempt struct {
	*antigravitylaunch.FirstInputAttempt
	beforeEnter func()
}

func (a firstInputTestAttempt) BeforeEnter(c context.Context) error {
	if err := a.FirstInputAttempt.BeforeEnter(c); err != nil {
		return err
	}
	if a.beforeEnter != nil {
		a.beforeEnter()
	}
	return nil
}

type firstInputTestPane struct{ *agyActionPane }

func (p firstInputTestPane) TypeAntigravityFirst(c context.Context, s session.Session, text string, check func(context.Context) error) error {
	if err := check(c); err != nil {
		return err
	}
	return p.Type(c, s, text)
}
func (p firstInputTestPane) EnterAntigravityFirst(c context.Context, s session.Session, check func(context.Context) error) error {
	if err := check(c); err != nil {
		return err
	}
	return p.Keystroke(c, s, []byte{'\r'})
}
func firstInputActions(t *testing.T) (Actions, *firstInputTestPort, *agyActionPane) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("private owner requires Unix")
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := antigravitylaunch.Open(antigravitylaunch.Config{StateDir: filepath.Join(root, "antigravity"), SquadLaunchRoot: filepath.Join(root, "squad", "launches")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close() })
	prepared, err := owner.Prepare(context.Background(), projects.LaunchRequest{Assistant: projects.AssistantAntigravity, ProjectRoot: root, Executable: "/synthetic/agy", PermissionMode: "plan"})
	if err != nil {
		t.Fatal(err)
	}
	a, p, b := agyActions()
	b.Process.ParentPID = 100
	b.Process.KernelGeneration = "synthetic-generation"
	b.Process.BootID = "synthetic-boot"
	if err = owner.AttachTerminal(context.Background(), prepared.Record.ID, "tmux", b.Process.TerminalID); err != nil {
		t.Fatal(err)
	}
	if err = owner.PinProcess(context.Background(), prepared.Record.ID, b.Process); err != nil {
		t.Fatal(err)
	}
	port := &firstInputTestPort{owner: owner, target: ports.AntigravityFirstInputTarget{LaunchID: prepared.Record.ID, Backend: "tmux", Process: b.Process}}
	native := firstInputTestPane{p}
	a.Terminals = []ports.TerminalHost{native}
	a.Inventory.Screen = native
	a.AntigravityFirstInput = port
	// The initial send must succeed without pretending a native UUID exists.
	p.s.ConversationID = ""
	p.s.Binding = ""
	a.Antigravity = nil
	return a, port, p
}

const bootstrapKey = "74000000-0000-4000-8000-000000000099"

func TestAntigravityFirstInputUsesOnePrivateGrantWithoutAConversation(t *testing.T) {
	a, port, p := firstInputActions(t)
	ctx := context.Background()
	got, err := a.SendAntigravityFirst(ctx, port.target.LaunchID, bootstrapKey, "hello")
	if err != nil || got.ConversationID != "" || got.Binding.Bound() || p.rawSends != 0 || len(p.pasted) != 1 || strings.Join(p.keys(), "") != "\r" {
		t.Fatalf("first input: %+v %v paste=%v keys=%v", got, err, p.pasted, p.keys())
	}
	p.show(agyActionComposer(""))
	_, err = a.SendAntigravityFirst(ctx, port.target.LaunchID, bootstrapKey, "hello")
	if err == nil || len(p.pasted) != 1 || len(p.keys()) != 1 {
		t.Fatal("identical retry replayed effects", err)
	}
	_, err = a.SendAntigravityFirst(ctx, port.target.LaunchID, "74000000-0000-4000-8000-000000000098", "changed")
	if err == nil || len(p.pasted) != 1 || len(p.keys()) != 1 {
		t.Fatal("new request replayed effects", err)
	}
	receipt, err := port.owner.FirstInputStatus(ctx, port.target.LaunchID, bootstrapKey)
	if err != nil || receipt.Phase != antigravitylaunch.FirstInputEnterAttempted {
		t.Fatal(receipt, err)
	}
}
func TestAntigravityFirstInputStopsAfterPasteOrDurableEnterFenceChanges(t *testing.T) {
	for _, kind := range []string{"process-after-paste", "authorization-after-paste", "draft-after-enter-fence", "authorization-after-enter-fence", "lost-enter-reply"} {
		t.Run(kind, func(t *testing.T) {
			a, port, p := firstInputActions(t)
			authorized := true
			ctx := WithOperationCheck(context.Background(), func() error {
				if !authorized {
					return errors.New("revoked")
				}
				return nil
			})
			switch kind {
			case "process-after-paste":
				p.afterPaste = func() { port.target.Process.KernelGeneration = "replacement" }
			case "authorization-after-paste":
				p.afterPaste = func() { authorized = false }
			case "draft-after-enter-fence":
				port.beforeEnter = func() { p.show(agyActionComposer("human draft")) }
			case "authorization-after-enter-fence":
				port.beforeEnter = func() { authorized = false }
			case "lost-enter-reply":
				p.keyError = errors.New("synthetic lost reply")
			}
			_, err := a.SendAntigravityFirst(ctx, port.target.LaunchID, bootstrapKey, "hello")
			if err == nil || len(p.pasted) != 1 || p.rawSends != 0 {
				t.Fatal("uncertain input accepted", err)
			}
			wantKeys := 0
			if kind == "lost-enter-reply" {
				wantKeys = 1
			}
			if len(p.keys()) != wantKeys {
				t.Fatal("effect continued after changed fence", p.keys())
			}
			p.show(agyActionComposer(""))
			authorized = true
			_, err = a.SendAntigravityFirst(ctx, port.target.LaunchID, bootstrapKey, "hello")
			if err == nil || len(p.pasted) != 1 || len(p.keys()) != wantKeys {
				t.Fatal("uncertain attempt was replayed", err)
			}
		})
	}
}
func TestAntigravityFirstInputRefusesBeforeReservationWhenNotReady(t *testing.T) {
	for _, kind := range []string{"draft", "menu", "shell", "slash", "unverified", "generic-host"} {
		t.Run(kind, func(t *testing.T) {
			a, port, p := firstInputActions(t)
			text := "hello"
			switch kind {
			case "draft":
				p.show(agyActionComposer("human"))
			case "menu":
				p.show(agyActionPermission(1))
			case "shell":
				text = "! touch synthetic"
			case "slash":
				text = "/clear"
			case "unverified":
				port.check = func() error { return errors.New("missing fresh proof") }
			case "generic-host":
				a.Terminals = []ports.TerminalHost{p}
			}
			_, err := a.SendAntigravityFirst(context.Background(), port.target.LaunchID, bootstrapKey, text)
			if err == nil || len(p.pasted) != 0 || len(p.keys()) != 0 || p.rawSends != 0 {
				t.Fatal("unsafe bootstrap wrote input", err)
			}
			stats, err := port.owner.Stats(context.Background())
			if err != nil || stats.FirstInputs != 0 {
				t.Fatal("preflight refusal consumed slot", stats, err)
			}
		})
	}
}
