//go:build darwin || linux

package process

import (
	"context"
	"errors"
	"testing"

	"github.com/sainteye/clawdline/internal/app/ports"
	"github.com/sainteye/clawdline/internal/domain/session"
)

type antigravityVerifierFixture struct {
	p   ports.AntigravityProcess
	err error
}

type knownIDE struct{}

func (knownIDE) Error() string                  { return "synthetic IDE" }
func (knownIDE) AntigravityNotStandalone() bool { return true }

func TestAntigravityDiscoveryKeepsUnreadableDistinctFromIDE(t *testing.T) {
	p := &PS{Antigravity: antigravityVerifierFixture{err: errors.New("unreadable")}}
	if _, ok, complete := p.antigravityProcessStatus(context.Background(), 41, "ttyA"); ok || complete {
		t.Fatal("unreadable candidate became authoritative absence")
	}
	p.Antigravity = antigravityVerifierFixture{err: knownIDE{}}
	if _, ok, complete := p.antigravityProcessStatus(context.Background(), 41, "ttyA"); ok || !complete {
		t.Fatal("proved IDE became an assistant or an incomplete scan")
	}
}

func (f antigravityVerifierFixture) VerifyAntigravityProcess(context.Context, int) (ports.AntigravityProcess, error) {
	return f.p, f.err
}
func TestAntigravityProcessRequiresFullProof(t *testing.T) {
	p := &PS{}
	if _, ok := p.antigravityProcess(context.Background(), 41, "ttyA"); ok {
		t.Fatal("nil verifier admitted IDE")
	}
	proof := ports.AntigravityProcess{PID: 41, Birth: "birth-a", TTY: "ttyA", TerminalID: "%1", Executable: "/synthetic/standalone/agy"}
	p.Antigravity = antigravityVerifierFixture{p: proof}
	if _, ok := p.antigravityProcess(context.Background(), 41, "ttyA"); !ok {
		t.Fatal("positive standalone proof refused")
	}
	for _, broken := range []ports.AntigravityProcess{
		{PID: 42, Birth: proof.Birth, TTY: proof.TTY, TerminalID: proof.TerminalID, Executable: proof.Executable},
		{PID: 41, TTY: proof.TTY, TerminalID: proof.TerminalID, Executable: proof.Executable},
		{PID: 41, Birth: proof.Birth, TTY: "ttyB", TerminalID: proof.TerminalID, Executable: proof.Executable},
		{PID: 41, Birth: proof.Birth, TTY: proof.TTY, Executable: proof.Executable},
	} {
		p.Antigravity = antigravityVerifierFixture{p: broken}
		if _, ok := p.antigravityProcess(context.Background(), 41, "ttyA"); ok {
			t.Fatalf("partial proof admitted %+v", broken)
		}
	}
	p.Antigravity = antigravityVerifierFixture{p: proof, err: errors.New("unreadable")}
	if _, ok := p.antigravityProcess(context.Background(), 41, "ttyA"); ok {
		t.Fatal("failed reader admitted proof")
	}
}

func TestAntigravityNameIsNotExecutableProof(t *testing.T) {
	for _, command := range []string{"agy", "/synthetic/ide/agy --new-window", "/synthetic/standalone/agy --conversation 11111111-1111-4111-8111-111111111111", "sh -c agy", "node agy"} {
		if got := classify(command); got != "" {
			t.Fatalf("name alone classified %q as %s", command, got)
		}
	}
	if classify("/synthetic/claude --resume synthetic") != session.AssistantClaude || classify("/synthetic/codex resume synthetic") != session.AssistantCodex {
		t.Fatal("existing executable classification regressed")
	}
}
